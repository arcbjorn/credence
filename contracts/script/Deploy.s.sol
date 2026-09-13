// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Script} from "forge-std/Script.sol";
import {console2} from "forge-std/console2.sol";

import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {IERC20} from "../src/IERC20.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {TestSettlementToken} from "../src/TestSettlementToken.sol";
import {Route} from "../src/Assessment.sol";

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

        RiskOracle oracle = new RiskOracle(operator, deploymentId);
        CreditVault vault = new CreditVault(IERC20(settlement), operator, GLOBAL_CAP);
        LoanManager manager = new LoanManager(IERC20(settlement), vault, oracle, operator);

        // Back-references, now that the manager exists.
        vault.setLoanManager(address(manager));
        oracle.setLoanManager(address(manager));

        // A facility must pin exactly one route. Development attestation is
        // the baseline; a CRE facility would be a separate deployment with
        // its own domain (`docs/05`).
        oracle.pinRoute(FACILITY_ID, Route.Development);
        oracle.setSigner(Route.Development, attestor, true);

        vm.stopBroadcast();

        _writeManifest(address(oracle), address(vault), address(manager),
            settlement, attestor, deploymentId);
    }

    function _writeManifest(
        address oracle,
        address vault,
        address manager,
        address settlement,
        address attestor,
        uint256 deploymentId
    ) private {
        string memory manifest = string.concat(
            "{\n",
            '  "chain_id": ', vm.toString(block.chainid), ",\n",
            '  "deployment_id": ', vm.toString(deploymentId), ",\n",
            '  "facility_id": ', vm.toString(FACILITY_ID), ",\n",
            '  "global_cap": "', vm.toString(GLOBAL_CAP), '",\n',
            '  "addresses": {\n',
            '    "oracle": "', vm.toString(oracle), '",\n',
            '    "vault": "', vm.toString(vault), '",\n',
            '    "loan_manager": "', vm.toString(manager), '",\n',
            '    "settlement": "', vm.toString(settlement), '"\n',
            "  },\n",
            '  "attestor": "', vm.toString(attestor), '"\n',
            "}\n"
        );

        string memory path = string.concat(
            "deployments/", vm.toString(block.chainid), ".json"
        );
        vm.writeFile(path, manifest);

        console2.log("oracle      ", oracle);
        console2.log("vault       ", vault);
        console2.log("loan manager", manager);
        console2.log("manifest    ", path);
    }
}
