// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {IERC20} from "../src/IERC20.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {TestSettlementToken} from "../src/TestSettlementToken.sol";
import {Route} from "../src/Assessment.sol";

/// Verifies the deployed wiring, since a mis-wired facility fails only when
/// the first loan is attempted — far from the cause.
contract DeployTest is Test {
    address constant OPERATOR = address(0xA1);
    address constant ATTESTOR = address(0xB2);
    uint256 constant DEPLOYMENT_ID = 7;
    uint256 constant FACILITY_ID = 1;
    uint256 constant GLOBAL_CAP = 100_000e6;

    TestSettlementToken token;
    RiskOracle oracle;
    CreditVault vault;
    LoanManager manager;

    function setUp() public {
        token = new TestSettlementToken();
        vm.startPrank(OPERATOR);
        oracle = new RiskOracle(OPERATOR, DEPLOYMENT_ID);
        vault = new CreditVault(IERC20(address(token)), OPERATOR, GLOBAL_CAP);
        manager = new LoanManager(IERC20(address(token)), vault, oracle, OPERATOR);
        vault.setLoanManager(address(manager));
        oracle.setLoanManager(address(manager));
        oracle.pinRoute(FACILITY_ID, Route.Development);
        oracle.setSigner(Route.Development, ATTESTOR, true);
        vm.stopPrank();
    }

    /// The circular dependency must resolve: each side knows the other.
    function test_backReferencesAreWired() public view {
        assertEq(vault.loanManager(), address(manager), "vault must know the manager");
        assertEq(oracle.loanManager(), address(manager), "oracle must know the manager");
        assertEq(address(manager.vault()), address(vault));
        assertEq(address(manager.oracle()), address(oracle));
    }

    /// A facility with no pinned route accepts nothing, so pinning is part
    /// of deployment rather than a later step someone might forget.
    function test_routeIsPinnedAndSignerAllowlisted() public view {
        assertEq(uint8(oracle.facilityRoute(FACILITY_ID)), uint8(Route.Development));
        assertTrue(oracle.routeSigner(Route.Development, ATTESTOR));
    }

    /// A development key must not be usable on the CRE route, even after a
    /// deployment that only pinned Development.
    function test_attestorIsNotAllowlistedOnOtherRoutes() public view {
        assertFalse(oracle.routeSigner(Route.CRE, ATTESTOR));
    }

    function test_vaultStartsInFundingPhase() public view {
        assertEq(uint8(vault.phase()), uint8(CreditVault.Phase.Funding));
        assertEq(vault.globalCap(), GLOBAL_CAP);
    }

    /// The token must be unmistakably a test asset (`docs/10`).
    function test_settlementTokenIsLabelledAsTest() public view {
        assertEq(token.symbol(), "tCRED");
        assertEq(token.decimals(), 6);
        // The name must not be readable as issuer-backed USDC.
        assertEq(token.name(), "Credence Test Settlement (NOT USDC)");
    }

    /// Anyone may mint: a worthless token has nothing to misrepresent.
    function test_anyoneCanMintTheTestToken() public {
        vm.prank(address(0xCAFE));
        token.mint(address(0xCAFE), 1_000e6);
        assertEq(token.balanceOf(address(0xCAFE)), 1_000e6);
    }

    /// An unlimited allowance is not decremented, matching common ERC-20
    /// behaviour so integrations behave identically here.
    function test_unlimitedAllowanceIsNotDecremented() public {
        address holder = address(0xD1);
        token.mint(holder, 1_000e6);
        vm.prank(holder);
        token.approve(address(this), type(uint256).max);

        token.transferFrom(holder, address(this), 400e6);
        assertEq(token.allowance(holder, address(this)), type(uint256).max);
    }

    /// The deployment is usable end to end: a lender can fund the vault and
    /// the operator can activate it.
    function test_deployedFacilityAcceptsDeposits() public {
        address lender = address(0xE5);
        token.mint(lender, 50_000e6);

        vm.startPrank(lender);
        token.approve(address(vault), 50_000e6);
        vault.deposit(50_000e6);
        vm.stopPrank();

        assertEq(vault.idleCash(), 50_000e6);

        vm.prank(OPERATOR);
        vault.activate();
        assertEq(uint8(vault.phase()), uint8(CreditVault.Phase.Active));
    }

    /// Only the operator may rewire a deployed facility.
    function test_outsiderCannotRewire() public {
        vm.prank(address(0xBAD));
        vm.expectRevert(CreditVault.NotOperator.selector);
        vault.setLoanManager(address(0xBAD));

        vm.prank(address(0xBAD));
        vm.expectRevert(RiskOracle.NotOperator.selector);
        oracle.setSigner(Route.Development, address(0xBAD), true);
    }
}
