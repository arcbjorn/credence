// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Script} from "forge-std/Script.sol";
import {console2} from "forge-std/console2.sol";

import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {IERC20} from "../src/IERC20.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {FixedPriceSource} from "../src/FixedPriceSource.sol";
import {IPriceSource} from "../src/IPriceSource.sol";
import {SpotVenue} from "../src/SpotVenue.sol";
import {StrategyAdapter} from "../src/StrategyAdapter.sol";
import {TestRiskyAsset} from "../src/TestRiskyAsset.sol";
import {TestSettlementToken} from "../src/TestSettlementToken.sol";
import {Class, Route} from "../src/Assessment.sol";

/// @title Deploy
/// @notice Deploys the prototype facility and wires it.
///
/// The wiring order is forced by a circular dependency: `LoanManager` needs
/// the vault and oracle at construction, while both need to know the loan
/// manager afterwards. Deploying the two first and setting back-references
/// keeps every reference immutable where it can be, rather than making all
/// three mutable to avoid one ordering constraint.
///
/// The script emits a deployment manifest so the gateway's config is
/// derived from what was actually deployed rather than transcribed by hand.
contract Deploy is Script {
    /// @dev Global cap on outstanding principal for the prototype epoch.
    uint256 constant GLOBAL_CAP = 100_000e6;
    /// @dev Facility the oracle pins a route for.
    uint256 constant FACILITY_ID = 1;

    /// @dev Declared valuation for the risky asset. A prototype has no
    /// oracle for a test asset, so the price is stated rather than
    /// presented as an observation.
    uint256 constant RISKY_PRICE = 2_000e6;
    uint32 constant PRICE_SCALE = 6;
    uint32 constant RISKY_DECIMALS = 18;
    /// @dev Tolerance against the independent price before a trade is
    /// refused as a bad fill.
    uint32 constant MAX_SLIPPAGE_BPS = 200;
    uint64 constant MAX_PRICE_AGE_SECONDS = 3_600;
    uint256 constant VENUE_LIQUIDITY = 1_000_000e6;
    uint256 constant VENUE_RISKY_LIQUIDITY = 1_000e18;

    function run() external {
        uint256 deployerKey = vm.envUint("DEPLOYER_KEY");
        address operator = vm.addr(deployerKey);

        // The attestation signer is separate from the deployer: the
        // operator holds a funded key, the signer only attests. Sharing one
        // key would mean a compromised gateway could also move funds.
        address attestor = vm.envAddress("ATTESTOR_ADDRESS");
        uint256 deploymentId = vm.envOr("DEPLOYMENT_ID", uint256(1));
        vm.startBroadcast(deployerKey);

        // Deploy a named test token unless one is supplied. `docs/10`
        // requires named test assets; the deployed token is explicitly not
        // USDC and anyone may mint it.
        address settlement = vm.envOr("SETTLEMENT_TOKEN", address(0));
        if (settlement == address(0)) {
            settlement = address(new TestSettlementToken());
        }
        // The one supported risky asset. Without it no account can hold a
        // concentrated book, so the denial beat could not be demonstrated.
        address risky = vm.envOr("RISKY_ASSET", address(0));
        if (risky == address(0)) {
            risky = address(new TestRiskyAsset());
        }

        // A scenario deployment: its evidence is honestly synthetic, so
        // ScenarioOnly is the class it issues and the class it may act on
        // (`docs/04`). A production deployment pins PrototypeEligible.
        RiskOracle oracle =
            new RiskOracle(operator, deploymentId, Class.ScenarioOnly);
        CreditVault vault = new CreditVault(IERC20(settlement), operator, GLOBAL_CAP);
        LoanManager manager = new LoanManager(IERC20(settlement), vault, oracle, operator);

        // Back-references, now that the manager exists.
        vault.setLoanManager(address(manager));
        oracle.setLoanManager(address(manager));

        // The one supported execution route. `docs/00`: one strategy
        // adapter. Without it a credit account cannot deploy principal,
        // which makes a loan indistinguishable from a deposit.
        SpotVenue venue = new SpotVenue(
            IERC20(settlement), IERC20(risky),
            RISKY_PRICE, PRICE_SCALE, RISKY_DECIMALS, operator);
        FixedPriceSource priceSource =
            new FixedPriceSource(RISKY_PRICE, PRICE_SCALE, operator);
        StrategyAdapter adapter = new StrategyAdapter(
            IERC20(settlement), IERC20(risky), venue,
            IPriceSource(address(priceSource)),
            MAX_SLIPPAGE_BPS, MAX_PRICE_AGE_SECONDS, RISKY_DECIMALS);

        // Seed the venue so trades can settle. A venue with no inventory
        // would fail for a reason unrelated to the protocol.
        TestSettlementToken(settlement).mint(address(venue), VENUE_LIQUIDITY);
        TestRiskyAsset(risky).mint(address(venue), VENUE_RISKY_LIQUIDITY);

        // A facility must pin exactly one route. Development attestation is
        // the baseline; a CRE facility would be a separate deployment with
        // its own domain (`docs/05`).
        // Recovery must sell this before a default may be declared,
        // otherwise an empty settlement balance reads as exhaustion while
        // the position is still there.
        manager.setCollateralAsset(risky, true);

        oracle.pinRoute(FACILITY_ID, Route.Development);
        oracle.setSigner(Route.Development, attestor, true);

        vm.stopBroadcast();

        _writeManifest(
            Deployed({
                oracle: address(oracle),
                vault: address(vault),
                manager: address(manager),
                settlement: settlement,
                risky: risky,
                venue: address(venue),
                priceSource: address(priceSource),
                adapter: address(adapter)
            }),
            attestor,
            deploymentId
        );
    }

    struct Deployed {
        address oracle;
        address vault;
        address manager;
        address settlement;
        address risky;
        address venue;
        address priceSource;
        address adapter;
    }

    function _writeManifest(
        Deployed memory d,
        address attestor,
        uint256 deploymentId
    ) private {
        string memory addresses = string.concat(
            '  "addresses": {\n',
            '    "oracle": "', vm.toString(d.oracle), '",\n',
            '    "vault": "', vm.toString(d.vault), '",\n',
            '    "loan_manager": "', vm.toString(d.manager), '",\n',
            '    "settlement": "', vm.toString(d.settlement), '",\n',
            '    "risky": "', vm.toString(d.risky), '",\n',
            '    "venue": "', vm.toString(d.venue), '",\n',
            '    "price_source": "', vm.toString(d.priceSource), '",\n',
            '    "adapter": "', vm.toString(d.adapter), '"\n',
            "  },\n"
        );

        string memory manifest = string.concat(
            "{\n",
            '  "chain_id": ', vm.toString(block.chainid), ",\n",
            '  "deployment_id": ', vm.toString(deploymentId), ",\n",
            '  "facility_id": ', vm.toString(FACILITY_ID), ",\n",
            '  "global_cap": "', vm.toString(GLOBAL_CAP), '",\n',
            '  "risky_price": "', vm.toString(RISKY_PRICE), '",\n',
            addresses,
            '  "attestor": "', vm.toString(attestor), '"\n',
            "}\n"
        );

        string memory path = string.concat(
            "deployments/", vm.toString(block.chainid), ".json"
        );
        vm.writeFile(path, manifest);

        console2.log("oracle      ", d.oracle);
        console2.log("vault       ", d.vault);
        console2.log("loan manager", d.manager);
        console2.log("adapter     ", d.adapter);
        console2.log("manifest    ", path);
    }
}
