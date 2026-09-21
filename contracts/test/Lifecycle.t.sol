// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {
    Assessment,
    Binding,
    Class,
    Decision,
    Kind,
    Permission,
    Route,
    Terms
} from "../src/Assessment.sol";
import {IERC20} from "../src/IERC20.sol";
import {MockERC20} from "./MockERC20.sol";
import {OracleHarness} from "./OracleHarness.sol";

/// Minimal adapter used to advance an account's state version.
contract NoopAdapter {
    function act() external {}

    /// A second entry point, so a test can present an action the
    /// origination assessment never pinned.
    function other() external {}
}

/// An adapter whose action fails, standing in for a trade that cannot be
/// filled. `docs/05`: revert everything if post-action checks fail.
contract FailingAdapter {
    error CannotFill();

    function act() external pure {
        revert CannotFill();
    }
}

/// Minimal ERC-721 ownership, enough to model a transfer.
contract MockIdentityRegistry {
    mapping(uint256 => address) private owners;

    function mint(uint256 tokenId, address to) external {
        owners[tokenId] = to;
    }

    function transfer(uint256 tokenId, address to) external {
        owners[tokenId] = to;
    }

    function ownerOf(uint256 tokenId) external view returns (address) {
        return owners[tokenId];
    }
}

contract LifecycleTest is Test {
    MockERC20 token;
    CreditVault vault;
    RiskOracle oracle;
    LoanManager manager;
    CreditAccount account;
    NoopAdapter noop;

    uint256 constant DEPLOYMENT = 7;
    uint256 constant FACILITY = 1;
    uint256 constant SIGNER_KEY = 0xD30;
    uint64 seq;

    address constant OPERATOR = address(0xA1);
    address constant OWNER = address(0xB2);
    address constant AGENT = address(0xC3);
    address constant KEEPER = address(0xD4);
    address constant LENDER_A = address(0xE5);
    address constant LENDER_B = address(0xF6);

    uint256 constant CAP = 100_000e6;

    function setUp() public {
        token = new MockERC20();
        vault = new CreditVault(IERC20(address(token)), OPERATOR, CAP);
        oracle = new RiskOracle(OPERATOR, DEPLOYMENT, Class.PrototypeEligible);
        manager = new LoanManager(IERC20(address(token)), vault, oracle, OPERATOR);

        vm.startPrank(OPERATOR);
        vault.setLoanManager(address(manager));
        oracle.setLoanManager(address(manager));
        oracle.pinRoute(FACILITY, Route.Development);
        oracle.setSigner(Route.Development, vm.addr(SIGNER_KEY), true);
        vm.stopPrank();

        account = new CreditAccount(address(manager), OWNER, AGENT, address(oracle));
        noop = new NoopAdapter();

        _fund(LENDER_A, 60_000e6);
        _fund(LENDER_B, 40_000e6);
        vm.prank(OPERATOR);
        vault.activate();

        // Agent equity, per docs/02: no equity, no credit in v0.
        token.mint(address(account), 5_000e6);
    }

    /// Publishes a signed quote for `acct` and originates against it, which
    /// is now the only way a loan can exist (`docs/05` step 6-8).
    function _originate(CreditAccount acct, uint256 principal, uint32 aprBps, uint64 term)
        internal
        returns (uint256 loanId)
    {
        return _originateOn(manager, oracle, acct, principal, aprBps, term);
    }

    /// Publishes the quote WITHOUT originating, so a test can wrap only the
    /// origination call in `vm.expectRevert`.
    function _publish(
        RiskOracle orc,
        CreditAccount acct,
        uint256 principal,
        uint32 aprBps,
        uint64 term
    ) internal returns (Binding memory b) {
        // HOLD-ONLY by default.
        //
        // Most tests here are about capacity, expiry, bindings or custody
        // rather than about an approved trade, and they draw through the
        // plain `originate`. Since 14 September that path accepts only a
        // quote with no bound action, so a default action hash would make
        // every one of them fail on a rule they are not testing.
        //
        // The tests that ARE about an action pass their hash explicitly to
        // the overload below.
        return _publish(orc, acct, principal, aprBps, term, bytes32(0));
    }

    /// A quote committing to one exact action, for the atomic
    /// origination path.
    function _publish(
        RiskOracle orc,
        CreditAccount acct,
        uint256 principal,
        uint32 aprBps,
        uint64 term,
        bytes32 actionHash
    ) internal returns (Binding memory b) {
        b = OracleHarness.binding(address(acct), acct.stateVersion(), FACILITY);
        Assessment memory a = OracleHarness.approvedQuote(
            b,
            ++seq,
            principal,
            aprBps,
            term,
            0,
            address(token),
            DEPLOYMENT,
            uint64(block.timestamp),
            actionHash
        );
        orc.accept(a, OracleHarness.sign(orc, a, SIGNER_KEY));
    }

    function _originateOn(
        LoanManager mgr,
        RiskOracle orc,
        CreditAccount acct,
        uint256 principal,
        uint32 aprBps,
        uint64 term
    ) internal returns (uint256 loanId) {
        Binding memory b = OracleHarness.binding(address(acct), acct.stateVersion(), FACILITY);
        Assessment memory a = OracleHarness.approvedQuote(
            b,
            ++seq,
            principal,
            aprBps,
            term,
            0,
            address(token),
            DEPLOYMENT,
            uint64(block.timestamp)
        );
        orc.accept(a, OracleHarness.sign(orc, a, SIGNER_KEY));
        return mgr.originate(b, principal);
    }

    /// The binding a loan was written under, rebuilt for calls that must
    /// consult the oracle. Matches what `_originate` signed.
    function _bindingFor(CreditAccount acct, uint64 stateVersion)
        internal
        pure
        returns (Binding memory)
    {
        return OracleHarness.binding(address(acct), stateVersion, FACILITY);
    }

    function _fund(address lender, uint256 amount) private {
        token.mint(lender, amount);
        vm.startPrank(lender);
        token.approve(address(vault), amount);
        vault.deposit(amount);
        vm.stopPrank();
    }

    // --- Interest parity with risk-core ---

    /// `docs/11`: amount, rate, and term arithmetic must reconcile across
    /// Go, Rust, frontend, and Solidity. This asserts the exact base-unit
    /// figure that `risk-core`'s test suite asserts for the same inputs.
    function test_interestMatchesRiskCore() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        vm.warp(block.timestamp + 10 days);
        // risk-core: full_term(5_000_000_000, 1200, 864000) == 16_438_357
        assertEq(manager.accruedInterest(loanId), 16_438_357);
        assertEq(manager.debtDue(loanId), 5_016_438_357);
    }

    /// docs/03: accrual stops at maturity. No late penalty, no compounding.
    function test_accrualStopsAtMaturity() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        vm.warp(block.timestamp + 10 days);
        uint256 atMaturity = manager.accruedInterest(loanId);
        vm.warp(block.timestamp + 100 days);
        assertEq(manager.accruedInterest(loanId), atMaturity);
    }

    // --- Happy path ---

    function test_repaidPathConservesFunds() public {
        uint256 before = token.balanceOf(address(vault));

        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        assertEq(vault.outstandingPrincipal(), 5_000e6);
        assertTrue(account.debtOutstanding());

        vm.warp(block.timestamp + 10 days);
        uint256 interest = manager.accruedInterest(loanId);

        manager.repay(loanId);

        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Repaid));
        assertEq(vault.outstandingPrincipal(), 0);
        assertEq(vault.collectedInterest(), interest);
        assertEq(token.balanceOf(address(vault)), before + interest);
        assertFalse(account.debtOutstanding());
    }

    /// docs/05: one active loan per account.
    function test_secondLoanBlockedWhileActive() public {
        _originate(account, 1_000e6, 1_200, 10 days);
        Binding memory b = _publish(oracle, account, 1_000e6, 1_200, 10 days);
        vm.expectRevert(LoanManager.AccountHasActiveLoan.selector);
        manager.originate(b, 1_000e6);
    }

    /// docs/05: a pause halts originations but never blocks repayment.
    function test_pauseBlocksOriginationNotRepayment() public {
        uint256 loanId = _originate(account, 1_000e6, 1_200, 10 days);

        vm.prank(OPERATOR);
        manager.setPaused(true);

        Binding memory b = _publish(oracle, account, 1_000e6, 1_200, 10 days);
        vm.expectRevert(LoanManager.Paused.selector);
        manager.originate(b, 1_000e6);

        manager.repay(loanId); // still open
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Repaid));
    }

    // --- Recovery and loss ---

    /// A silent attestor must not make a healthy loan seizable.
    ///
    /// `riskAllowed` goes false the moment a monitoring window lapses, and
    /// recovery once read that as "permission withdrawn". An operator who
    /// also controls the attestor could then seize a healthy, unmatured
    /// position by withholding updates -- making the borrower's real
    /// covenant "keep the attestor answering" rather than "stay solvent".
    ///
    /// `docs/05` invariant 12: recovery needs a trigger. Silence is not
    /// one, at least not immediately. Reported by review, 14 September.
    function test_LapsedPermissionAloneDoesNotOpenRecovery() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        // A monitoring update grants standing permission, as the
        // continuous-underwriting loop does on every healthy cycle.
        Binding memory rb = _bindingFor(account, account.stateVersion());
        Assessment memory ok =
            OracleHarness.monitoringAllowed(rb, 98, DEPLOYMENT, uint64(block.timestamp));
        oracle.accept(ok, OracleHarness.sign(oracle, ok, SIGNER_KEY));
        assertTrue(oracle.riskAllowed(rb), "permission stands");

        // Then the attestor goes quiet and the window closes. Nothing
        // else happens: no denial, no market move, no maturity.
        vm.warp(block.timestamp + 2 hours);
        assertFalse(oracle.riskAllowed(rb), "permission has lapsed");
        assertFalse(
            oracle.permissionWithdrawn(rb),
            "but nothing was withdrawn -- the attestor simply went quiet"
        );

        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.LoanNotRecoverable.selector, loanId));
        manager.openRecovery(loanId, KEEPER, rb);
    }

    /// Silence cannot hold a loan open forever either. Past the grace
    /// window an unassessed obligation becomes recoverable, because
    /// nobody is watching it.
    function test_LapsedPermissionOpensRecoveryAfterGrace() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        Assessment memory ok =
            OracleHarness.monitoringAllowed(rb, 98, DEPLOYMENT, uint64(block.timestamp));
        oracle.accept(ok, OracleHarness.sign(oracle, ok, SIGNER_KEY));

        vm.warp(block.timestamp + 1 hours + manager.recoveryGraceSeconds() + 1);
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);
        assertEq(
            uint8(manager.status(loanId)),
            uint8(LoanManager.Status.RecoveryPending),
            "an obligation nobody assesses does not stay open indefinitely"
        );
    }

    /// An older denial must not seize a loan approved AFTER it.
    ///
    /// `_record` cleared permission state on a denial and set
    /// `permissionDenied`, but the APPROVED branch never cleared it again.
    /// A binding that was once denied therefore carried that flag forever,
    /// and the next loan it originated was recoverable in the block it was
    /// funded -- `permissionWithdrawn` still reported a withdrawal that
    /// referred to a superseded assessment.
    ///
    /// The operator controls both the denial and the recovery, so this is
    /// a seizure path that needs no borrower misstep at all.
    ///
    /// Reported by review, 15 September.
    function test_OlderDenialDoesNotSeizeANewlyApprovedLoan() public {
        // A monitoring denial on this binding.
        Binding memory rb = _bindingFor(account, account.stateVersion());
        Assessment memory blocked =
            OracleHarness.monitoringBlocked(rb, ++seq, DEPLOYMENT, uint64(block.timestamp));
        oracle.accept(blocked, OracleHarness.sign(oracle, blocked, SIGNER_KEY));
        assertTrue(oracle.permissionWithdrawn(rb), "the denial was recorded");

        // Then the engine approves a new loan for the same binding.
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        Binding memory nb = _bindingFor(account, account.stateVersion());
        assertFalse(oracle.permissionWithdrawn(nb), "an approval supersedes the earlier denial");

        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.LoanNotRecoverable.selector, loanId));
        manager.openRecovery(loanId, KEEPER, nb);
    }

    /// A previous loan's expired window must not seize the next loan.
    ///
    /// `silenceSince` reads `permissionExpiry`, which survived the loan it
    /// belonged to. After a first loan was monitored and repaid, the next
    /// loan inherited that stale expiry -- already far past the 12-hour
    /// grace -- so it was recoverable immediately, and the grace window
    /// that exists to protect a borrower from silence protected nothing.
    ///
    /// Reported by review, 15 September.
    function test_PreviousLoanExpiryDoesNotSeizeTheNextLoan() public {
        uint256 first = _originate(account, 5_000e6, 1_200, 10 days);

        // Monitored once, so an expiry is recorded against this binding.
        Binding memory rb = _bindingFor(account, account.stateVersion());
        Assessment memory ok =
            OracleHarness.monitoringAllowed(rb, ++seq, DEPLOYMENT, uint64(block.timestamp));
        oracle.accept(ok, OracleHarness.sign(oracle, ok, SIGNER_KEY));
        assertTrue(oracle.permissionExpiry(rb) != 0, "an expiry now stands");

        // The window closes and the grace period passes, then the loan is
        // repaid -- a clean, complete obligation.
        vm.warp(block.timestamp + 1 hours + manager.recoveryGraceSeconds() + 1);
        manager.repay(first);

        // A second loan is approved and funded now.
        uint256 second = _originate(account, 5_000e6, 1_200, 10 days);
        Binding memory nb = _bindingFor(account, account.stateVersion());

        // Its own grace has not begun to run, so nothing licenses recovery.
        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.LoanNotRecoverable.selector, second));
        manager.openRecovery(second, KEEPER, nb);
    }

    /// A loan that was NEVER monitored must not be seizable on origination.
    ///
    /// The gap the lapse fix left open. `permissionWithdrawn` answered the
    /// question "is there a standing window?" -- and a loan that has only
    /// ever been originated has none, because origination writes a quote
    /// and never a permission. So `permissionValidUntil` is zero, the
    /// withdrawal branch returned true, and the loan was recoverable the
    /// block it was funded.
    ///
    /// That is the same defect as the lapse, in its worst form: no
    /// statement was ever made about this borrower, and silence became a
    /// trigger instantly rather than after the grace window. An operator
    /// could fund a loan and seize it in the next transaction.
    ///
    /// `docs/05` invariant 12: recovery needs a trigger. Reported by
    /// review, 14 September.
    function test_NeverMonitoredLoanIsNotImmediatelyRecoverable() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        // No monitoring assessment is ever accepted. This is the state of
        // every loan between origination and its first monitoring cycle.
        Binding memory rb = _bindingFor(account, account.stateVersion());
        assertEq(oracle.permissionExpiry(rb), 0, "nothing was ever granted");
        assertFalse(
            oracle.permissionWithdrawn(rb),
            "nothing was ever withdrawn either -- nothing was ever said"
        );

        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.LoanNotRecoverable.selector, loanId));
        manager.openRecovery(loanId, KEEPER, rb);
    }

    /// Nor can a never-monitored loan stay open forever. The grace window
    /// runs from ORIGINATION when no permission was ever issued, so the
    /// same "nobody is watching this" backstop applies.
    function test_NeverMonitoredLoanRecoverableAfterGrace() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        Binding memory rb = _bindingFor(account, account.stateVersion());

        vm.warp(block.timestamp + manager.recoveryGraceSeconds() + 1);

        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);
        assertEq(
            uint8(manager.status(loanId)),
            uint8(LoanManager.Status.RecoveryPending),
            "an unassessed obligation does not stay open indefinitely"
        );
    }

    /// An explicit denial is a statement about the loan, and triggers at
    /// once. The grace period protects against silence, not against a
    /// risk decision.
    function test_WithdrawnPermissionOpensRecoveryImmediately() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        Assessment memory blocked =
            OracleHarness.monitoringBlocked(rb, 99, DEPLOYMENT, uint64(block.timestamp));
        oracle.accept(blocked, OracleHarness.sign(oracle, blocked, SIGNER_KEY));

        assertTrue(oracle.permissionWithdrawn(rb), "a denial is a statement, not silence");

        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));
    }

    /// `docs/05` invariant 8: **only the appointed keeper** collects.
    ///
    /// `collect` also accepted the operator, while the comment directly
    /// above the check said "Only the appointed keeper collects" -- a
    /// contradiction inside one statement, and a widening of the operator
    /// powers `docs/05` enumerates. It grants nothing the operator cannot
    /// already reach, since the operator appoints the keeper in
    /// `openRecovery`; it just makes the documented invariant false and
    /// removes the appointment step from the audit trail.
    ///
    /// Reported by review, 14 September.
    function test_OperatorCannotCollectWithoutBeingKeeper() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 11 days); // matured

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);

        // The operator appointed KEEPER and is not itself the keeper.
        assertEq(account.recoveryKeeper(), KEEPER, "the keeper is who openRecovery appointed");

        vm.prank(OPERATOR);
        vm.expectRevert(LoanManager.NotKeeper.selector);
        manager.collect(loanId, 1_000e6);

        // The appointed keeper still can, so this narrows authority
        // rather than breaking recovery.
        vm.prank(KEEPER);
        manager.collect(loanId, 1_000e6);
    }

    /// docs/05: collection accounting is idempotent across several calls.
    function test_recoveryCollectsInStages() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        // The whole obligation is unresolved, principal and frozen
        // interest alike: the epoch cannot settle while either is owed.
        uint256 owed = manager.debtDue(loanId);
        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);
        assertEq(vault.unresolvedReceivables(), owed);

        vm.prank(KEEPER);
        manager.collect(loanId, 2_000e6);
        assertEq(vault.unresolvedReceivables(), owed - 2_000e6);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));

        uint256 rest = manager.debtDue(loanId);
        vm.prank(KEEPER);
        manager.collect(loanId, rest);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Recovered));
        assertEq(vault.unresolvedReceivables(), 0);
    }

    /// docs/02 worked loss path: the shortfall lands on lenders, and
    /// principal loss is recorded separately from unpaid interest.
    function test_defaultAllocatesLossToVault() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);

        // Shock: only 3,940 recoverable against 5,016.44 of debt.
        uint256 accountBalance = token.balanceOf(address(account));
        vm.prank(address(manager));
        account.sweepTo(address(token), address(0xDEAD), accountBalance - 3_940e6);

        vm.prank(KEEPER);
        manager.collect(loanId, 3_940e6);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));

        vm.prank(OPERATOR);
        manager.declareDefault(loanId);

        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Defaulted));
        // 5,000 principal less 3,940 collected = 1,060 loss (docs/02).
        assertEq(vault.realizedLoss(), 1_060e6);
        assertEq(vault.unresolvedReceivables(), 0);
        assertTrue(manager.defaulted(address(account)));
    }

    /// docs/05: a binding with an unresolved default takes no new loans.
    function test_defaultedBindingBlockedFromNewCredit() public {
        _driveToDefault();
        Binding memory b = _publish(oracle, account, 1_000e6, 1_200, 10 days);
        vm.expectRevert(LoanManager.AccountInDefault.selector);
        manager.originate(b, 1_000e6);
    }

    /// docs/05: unavailable liquidity leaves the loan RecoveryPending. It is
    /// never resolved because a timeout elapsed.
    function test_cannotDefaultWhileAssetsRemain() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        vm.startPrank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, _bindingFor(account, account.stateVersion()));
        vm.expectRevert(LoanManager.NotYetDefaultable.selector);
        manager.declareDefault(loanId);
        vm.stopPrank();
    }

    // --- Vault epoch and settlement ---

    /// docs/05: the epoch cannot settle while a receivable is unresolved.
    function test_settleBlockedWhileReceivableOpen() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        vm.startPrank(OPERATOR);
        vault.beginResolving();
        vm.expectRevert(abi.encodeWithSelector(CreditVault.ReceivablesUnresolved.selector, 5_000e6));
        vault.settle();
        vm.stopPrank();

        manager.repay(loanId);
        vm.prank(OPERATOR);
        vault.settle();
        assertEq(uint8(vault.phase()), uint8(CreditVault.Phase.Settled));
    }

    /// docs/05: lenders withdraw actual final cash; dust reaches the last
    /// claimant rather than being stranded.
    function test_settlementDistributesAllCashIncludingDust() public {
        _driveToDefault();

        vm.startPrank(OPERATOR);
        vault.beginResolving();
        vault.settle();
        vm.stopPrank();

        uint256 pot = vault.settlementCash();
        vm.prank(LENDER_A);
        uint256 a = vault.claim();
        vm.prank(LENDER_B);
        uint256 b = vault.claim();

        assertEq(a + b, pot, "all cash distributed");
        assertEq(token.balanceOf(address(vault)), 0, "no dust stranded");
        // Loss was borne by lenders: less out than in.
        assertLt(pot, 100_000e6);
    }

    /// docs/05: deposits close before lending begins.
    function test_depositRejectedAfterFunding() public {
        token.mint(LENDER_A, 1e6);
        vm.startPrank(LENDER_A);
        token.approve(address(vault), 1e6);
        vm.expectRevert(
            abi.encodeWithSelector(
                CreditVault.WrongPhase.selector, CreditVault.Phase.Funding, CreditVault.Phase.Active
            )
        );
        vault.deposit(1e6);
        vm.stopPrank();
    }

    /// The cap binds before idle cash does. Deposits here exceed the cap, so
    /// a request between the two must fail on the cap specifically.
    function test_lendingRespectsGlobalCap() public {
        CreditVault capped = new CreditVault(IERC20(address(token)), OPERATOR, 10_000e6);
        RiskOracle cappedOracle = new RiskOracle(OPERATOR, DEPLOYMENT, Class.PrototypeEligible);
        LoanManager cappedManager =
            new LoanManager(IERC20(address(token)), capped, cappedOracle, OPERATOR);
        vm.startPrank(OPERATOR);
        capped.setLoanManager(address(cappedManager));
        cappedOracle.setLoanManager(address(cappedManager));
        cappedOracle.pinRoute(FACILITY, Route.Development);
        cappedOracle.setSigner(Route.Development, vm.addr(SIGNER_KEY), true);
        vm.stopPrank();

        token.mint(LENDER_A, 50_000e6);
        vm.startPrank(LENDER_A);
        token.approve(address(capped), 50_000e6);
        capped.deposit(50_000e6);
        vm.stopPrank();
        vm.prank(OPERATOR);
        capped.activate();

        CreditAccount acct =
            new CreditAccount(address(cappedManager), OWNER, AGENT, address(cappedOracle));
        // Funded, so the cap is what binds rather than the equity rule.
        token.mint(address(acct), 5_000e6);
        Binding memory b = _publish(cappedOracle, acct, 10_001e6, 1_200, 10 days);
        vm.expectRevert(CreditVault.ExceedsGlobalCap.selector);
        cappedManager.originate(b, 10_001e6);
    }

    // --- Authorization is load-bearing ---

    /// Without a standing quote there is no loan. The operator cannot
    /// originate on trust alone.
    function test_originationRequiresQuote() public {
        Binding memory b = OracleHarness.binding(address(account), account.stateVersion(), FACILITY);
        vm.expectRevert(RiskOracle.NoQuote.selector);
        manager.originate(b, 5_000e6);
    }

    /// `docs/03`: exact requested amount or denial. A quote for 5,000 cannot
    /// fund 4,000.
    function test_drawMustMatchApprovedAmount() public {
        Binding memory b = _publish(oracle, account, 5_000e6, 1_200, 10 days);
        vm.expectRevert(
            abi.encodeWithSelector(RiskOracle.AmountMismatch.selector, 5_000e6, 4_000e6)
        );
        manager.originate(b, 4_000e6);
    }

    /// A quote is consumed once. The second draw finds nothing standing.
    function test_quoteCannotFundTwoLoans() public {
        Binding memory b = _publish(oracle, account, 1_000e6, 1_200, 10 days);
        uint256 loanId = manager.originate(b, 1_000e6);
        manager.repay(loanId);

        vm.expectRevert(RiskOracle.QuoteConsumed.selector);
        manager.originate(b, 1_000e6);
    }

    /// Funding and the approved trade happen together.
    ///
    /// `docs/05`: "Where an initial trade is part of the approval, fund and
    /// trade atomically." Two transactions leave a gap the assessment did
    /// not price -- the engine sized the loan against the portfolio the
    /// action WOULD produce, so a borrower that drew and never executed
    /// would hold settlement tokens the stress scenarios assumed deployed.
    ///
    /// Reported by review, 14 September.
    function test_OriginateAndExecuteFundsAndTradesTogether() public {
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), true);

        Binding memory b = _publish(
            oracle,
            account,
            1_000e6,
            1_200,
            10 days,
            keccak256(abi.encode(address(noop), abi.encodeCall(NoopAdapter.act, ())))
        );

        vm.prank(AGENT);
        (uint256 loanId,) = manager.originateAndExecute(
            b, 1_000e6, address(noop), abi.encodeCall(NoopAdapter.act, ())
        );

        assertEq(
            uint8(manager.status(loanId)), uint8(LoanManager.Status.Active), "the loan is live"
        );
        assertEq(account.stateVersion(), 1, "and the action ran inside the same transaction");
    }

    /// Permission, not the pinned hash, gates what comes after.
    ///
    /// Driven through `LoanManager` because that is the only production
    /// caller of `openDebt`. `CreditAccount.t.sol` used to isolate this by
    /// calling `openDebt` a second time to re-pin the same action -- a
    /// path that cannot occur, and the reason a loan frozen to one action
    /// for its life went unnoticed.
    function test_ExecuteAfterOriginationRestsOnPermission() public {
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), true);

        Binding memory b = _publish(
            oracle,
            account,
            1_000e6,
            1_200,
            10 days,
            keccak256(abi.encode(address(noop), abi.encodeCall(NoopAdapter.act, ())))
        );

        vm.prank(AGENT);
        manager.originateAndExecute(b, 1_000e6, address(noop), abi.encodeCall(NoopAdapter.act, ()));

        // The action bumped `stateVersion`, which voids the permission it
        // ran under -- invariant 3: the portfolio that assessment was
        // computed against no longer exists. A second action needs a
        // FRESH monitoring result, not a standing bit.
        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.RiskNotPermitted.selector);
        account.execute(address(noop), abi.encodeCall(NoopAdapter.other, ()));

        // The continuous-underwriting loop re-assesses the account as it
        // now stands, which is what grants standing permission.
        Binding memory rb = _bindingFor(account, account.stateVersion());
        Assessment memory ok =
            OracleHarness.monitoringAllowed(rb, ++seq, DEPLOYMENT, uint64(block.timestamp));
        oracle.accept(ok, OracleHarness.sign(oracle, ok, SIGNER_KEY));

        // Now a DIFFERENT action runs (`docs/05`:76). Before the fix this
        // reverted `ActionNotPermitted` however fresh the assessment was:
        // the pinned hash gated the life of the loan, not just its
        // origination.
        vm.prank(AGENT);
        account.execute(address(noop), abi.encodeCall(NoopAdapter.other, ()));
        assertEq(account.stateVersion(), 2, "the second action ran");
    }

    /// A trade that cannot be filled unwinds the draw with it.
    ///
    /// The alternative is a funded loan whose approved action never
    /// happened -- exactly the position the atomicity rule exists to
    /// prevent.
    function test_FailedActionUnwindsTheDraw() public {
        FailingAdapter bad = new FailingAdapter();
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(bad), true);

        Binding memory b = _publish(
            oracle,
            account,
            1_000e6,
            1_200,
            10 days,
            keccak256(abi.encode(address(bad), abi.encodeCall(FailingAdapter.act, ())))
        );

        uint256 cashBefore = vault.idleCash();

        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.AdapterCallFailed.selector);
        manager.originateAndExecute(
            b, 1_000e6, address(bad), abi.encodeCall(FailingAdapter.act, ())
        );

        assertEq(manager.activeLoan(address(account)), 0, "no loan survives a failed action");
        assertEq(vault.idleCash(), cashBefore, "and no cash left the vault");
        assertFalse(account.debtOutstanding(), "nor any custody lock");
    }

    /// A quote bound to an action must not be drawable without it.
    ///
    /// `originateAndExecute` unwinds a failed trade, but it is not the only
    /// door. `originate` is public and consumes the SAME quote, so a
    /// borrower whose approved trade would revert can simply call the other
    /// entry point and keep the cash -- holding settlement tokens the
    /// stress scenarios assumed were deployed.
    ///
    /// That is not untidy, it is a different risk position than the one
    /// priced. The atomicity rule `docs/05` states ("fund and trade
    /// atomically; revert everything if post-action checks fail") is only
    /// enforced for callers who opt into it.
    ///
    /// A quote whose approved action is to HOLD is the legitimate use of
    /// the plain entry point, and that case is what `permittedActionHash
    /// == 0` now means.
    ///
    /// Reported by review, 14 September.
    function test_ActionBoundQuoteCannotBeDrawnWithoutTheAction() public {
        FailingAdapter bad = new FailingAdapter();
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(bad), true);

        Binding memory b = _publish(
            oracle,
            account,
            1_000e6,
            1_200,
            10 days,
            keccak256(abi.encode(address(bad), abi.encodeCall(FailingAdapter.act, ())))
        );

        // The approved trade cannot be filled.
        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.AdapterCallFailed.selector);
        manager.originateAndExecute(
            b, 1_000e6, address(bad), abi.encodeCall(FailingAdapter.act, ())
        );

        // So the borrower takes the other door. It must be shut.
        vm.prank(AGENT);
        vm.expectRevert(LoanManager.ActionRequired.selector);
        manager.originate(b, 1_000e6);

        assertEq(
            manager.activeLoan(address(account)),
            0,
            "an action-bound quote funded nothing without its action"
        );
    }

    /// A hold-only quote still originates through the plain entry point.
    ///
    /// The narrowing above must not break the case it exists to permit:
    /// when there is no approved action to perform, there is nothing to
    /// perform atomically.
    function test_HoldOnlyQuoteOriginatesDirectly() public {
        Binding memory b = _publish(oracle, account, 1_000e6, 1_200, 10 days, bytes32(0));

        vm.prank(AGENT);
        uint256 loanId = manager.originate(b, 1_000e6);

        assertEq(
            manager.activeLoan(address(account)),
            loanId,
            "a hold-only quote funds without an action"
        );
    }

    /// A de-risking exemption may not be granted while the account owes.
    ///
    /// Sharper than adding an adapter: a route added mid-loan still needs
    /// standing permission to act, while a selector relabelled as
    /// de-risking needs none at all. An operator could otherwise exempt a
    /// risk-INCREASING call from the permission gate on a live loan --
    /// which is the hazard the function's own comment names.
    ///
    /// `docs/05` invariant 11. Reported by review, 14 September.
    function test_DeRiskingLabelRefusedWhileAccountOwes() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        assertEq(manager.activeLoan(address(account)), loanId, "the loan is live");

        vm.prank(OPERATOR);
        vm.expectRevert(LoanManager.AccountHasActiveLoan.selector);
        manager.setAccountDeRiskingAction(account, address(noop), NoopAdapter.act.selector, true);
    }

    /// Withdrawing one stays open. Taking an exemption away only ever
    /// narrows what the account can do, so it is safe under a live loan --
    /// the same asymmetry revocation has for adapters.
    function test_DeRiskingLabelCanAlwaysBeWithdrawn() public {
        vm.prank(OPERATOR);
        manager.setAccountDeRiskingAction(account, address(noop), NoopAdapter.act.selector, true);

        _originate(account, 5_000e6, 1_200, 10 days);

        vm.prank(OPERATOR);
        manager.setAccountDeRiskingAction(account, address(noop), NoopAdapter.act.selector, false);
        assertFalse(
            account.deRiskingAction(address(noop), NoopAdapter.act.selector),
            "revocation is always permitted"
        );
    }

    /// The binding must describe the account as it stands. If the account
    /// executed after assessment, the assessed portfolio no longer exists.
    function test_staleAccountStateRejected() public {
        Binding memory b = _publish(oracle, account, 1_000e6, 1_200, 10 days);

        // Account acts between assessment and draw, the way it really would.
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), true);
        vm.prank(AGENT);
        account.execute(address(noop), abi.encodeCall(NoopAdapter.act, ()));

        vm.expectRevert(abi.encodeWithSelector(LoanManager.StaleAccountState.selector, 0, 1));
        manager.originate(b, 1_000e6);
    }

    /// `docs/02`: an agent with no equity receives no credit in v0.
    function test_insufficientEquityRejected() public {
        CreditAccount poor = new CreditAccount(address(manager), OWNER, AGENT, address(oracle));
        // Some equity, but less than the quote requires: this test is
        // about the SHORTFALL, not about the empty-account rule.
        token.mint(address(poor), 1_000e6);
        Binding memory b = OracleHarness.binding(address(poor), poor.stateVersion(), FACILITY);
        Assessment memory a = OracleHarness.approvedQuote(
            b,
            ++seq,
            1_000e6,
            1_200,
            10 days,
            5_000e6,
            address(token),
            DEPLOYMENT,
            uint64(block.timestamp)
        );
        oracle.accept(a, OracleHarness.sign(oracle, a, SIGNER_KEY));

        vm.expectRevert(
            abi.encodeWithSelector(LoanManager.InsufficientEquity.selector, 5_000e6, 1_000e6)
        );
        manager.originate(b, 1_000e6);
    }

    function _driveToDefault() private {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);
        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);

        uint256 bal = token.balanceOf(address(account));
        vm.prank(address(manager));
        account.sweepTo(address(token), address(0xDEAD), bal - 3_940e6);
        vm.prank(KEEPER);
        manager.collect(loanId, 3_940e6);

        vm.prank(OPERATOR);
        manager.declareDefault(loanId);
    }

    // --- Recovery collection bounds ---

    /// Recovery may only take what the loan is owed.
    ///
    /// `docs/02` close-out ordering: the agent's residual equity is the
    /// borrower's property. Sweeping the whole account and booking the
    /// surplus as interest confiscates it, and leaves interestCollected
    /// above what ever accrued -- which makes debtDue underflow.
    function test_CollectCannotTakeResidualEquity() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 1 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, OPERATOR, rb);

        uint256 owed = manager.debtDue(loanId);
        uint256 held = token.balanceOf(address(account));
        assertGt(
            held, owed, "the account must hold more than it owes for this test to mean anything"
        );

        // Ask for everything; only the obligation may move.
        vm.prank(OPERATOR);
        manager.collect(loanId, held);

        assertEq(
            token.balanceOf(address(account)),
            held - owed,
            "residual equity must stay with the borrower"
        );
        assertEq(manager.debtDue(loanId), 0, "a settled loan owes nothing");
    }

    /// An open recovery must not let an arbitrary caller move the
    /// account's balance. Collection belongs to the appointed keeper.
    function test_CollectIsRestrictedToTheKeeper() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 1 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, OPERATOR, rb);

        vm.prank(address(0xBAD));
        vm.expectRevert(LoanManager.NotKeeper.selector);
        manager.collect(loanId, 1);
    }

    /// A fully repaid loan must not keep accruing in views. Interest is
    /// charged for time the principal was actually outstanding.
    function test_RepaidLoanStopsAccruing() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 1 days);
        manager.repay(loanId);

        assertEq(manager.debtDue(loanId), 0, "a repaid loan owes nothing");

        vm.warp(block.timestamp + 2 days);
        assertEq(manager.debtDue(loanId), 0, "a repaid loan must not accrue phantom debt");
    }

    // --- Binding integrity (docs/05) ---

    /// A quote must fund only the binding it was signed for.
    ///
    /// The standing-quote key covers registry, agent, account and facility
    /// -- a deliberately stable identity. But the rest of the binding is
    /// signed too, and it is supplied by the caller at consumption. Without
    /// a commitment to the whole signed binding, an account can execute to
    /// bump its state version, then present the new version alongside
    /// altered configuration and policy fields and still draw on the old
    /// quote. The stale-state check passes because it compares the
    /// caller's own number with the account.
    function test_QuoteCannotFundARewrittenBinding() public {
        Binding memory signed = _publish(oracle, account, 5_000e6, 1_200, 10 days);

        // The account acts, so the assessed portfolio no longer exists.
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), true);
        vm.prank(AGENT);
        account.execute(address(noop), abi.encodeCall(NoopAdapter.act, ()));

        // Present the new state version with rewritten config and policy.
        Binding memory rewritten = signed;
        rewritten.accountStateVersion = account.stateVersion();
        rewritten.configVersion = signed.configVersion + 1;
        rewritten.configHash = keccak256("rewritten-config");
        rewritten.policyEpoch = signed.policyEpoch + 1;
        rewritten.policyHash = keccak256("rewritten-policy");

        vm.expectRevert(RiskOracle.BindingMismatch.selector);
        manager.originate(rewritten, 5_000e6);
    }

    /// The unaltered binding still funds, so the commitment bounds what
    /// may change rather than blocking origination outright.
    function test_UnalteredBindingStillFunds() public {
        Binding memory b = _publish(oracle, account, 5_000e6, 1_200, 10 days);
        uint256 loanId = manager.originate(b, 5_000e6);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Active));
    }

    // --- Recovery resolution (docs/05) ---

    /// A default must not be declared while collateral remains unsold.
    ///
    /// An empty settlement balance is not exhaustion. Declaring default
    /// here writes off lender principal and then unlocks the very
    /// positions recovery existed to sell -- the owner withdraws them.
    function test_DefaultRequiresCollateralResolved() public {
        MockERC20 risky = new MockERC20();
        vm.prank(OPERATOR);
        manager.setCollateralAsset(address(risky), true);

        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);

        // Settlement is exhausted, but the account still holds collateral.
        uint256 bal = token.balanceOf(address(account));
        vm.prank(address(manager));
        account.sweepTo(address(token), address(0xDEAD), bal);
        risky.mint(address(account), 1e18);

        vm.prank(OPERATOR);
        vm.expectRevert(
            abi.encodeWithSelector(LoanManager.CollateralUnresolved.selector, address(risky))
        );
        manager.declareDefault(loanId);
    }

    /// Once the collateral really is gone, the default proceeds.
    function test_DefaultProceedsOnceCollateralIsSold() public {
        MockERC20 risky = new MockERC20();
        vm.prank(OPERATOR);
        manager.setCollateralAsset(address(risky), true);

        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);

        uint256 bal = token.balanceOf(address(account));
        vm.prank(address(manager));
        account.sweepTo(address(token), address(0xDEAD), bal);

        vm.prank(OPERATOR);
        manager.declareDefault(loanId);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Defaulted));
    }

    /// The epoch must not settle while interest is still being recovered.
    ///
    /// Marking only principal unresolved lets settlement freeze
    /// settlementCash the moment principal is collected -- and interest
    /// collected afterwards can never be fairly included in it.
    function test_VaultCannotSettleWhileInterestRecovers() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);

        // Collect exactly the principal, leaving interest outstanding.
        vm.prank(KEEPER);
        manager.collect(loanId, 5_000e6);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));

        vm.startPrank(OPERATOR);
        vault.beginResolving();
        vm.expectRevert(
            abi.encodeWithSelector(
                CreditVault.ReceivablesUnresolved.selector, vault.unresolvedReceivables()
            )
        );
        vault.settle();
        vm.stopPrank();
    }

    // --- The cap bounds the obligation, not the disbursement (docs/05) ---

    /// A loan must not fill the whole cap with principal alone.
    ///
    /// Reserving nothing for interest means the epoch owes more than its
    /// declared budget the moment accrual begins -- the cap stops being a
    /// bound on exposure and becomes a bound on disbursement only.
    function test_GlobalCapReservesFullTermInterest() public {
        CreditVault capped = new CreditVault(IERC20(address(token)), OPERATOR, 10_000e6);
        RiskOracle cappedOracle = new RiskOracle(OPERATOR, DEPLOYMENT, Class.PrototypeEligible);
        LoanManager cappedManager =
            new LoanManager(IERC20(address(token)), capped, cappedOracle, OPERATOR);
        vm.startPrank(OPERATOR);
        capped.setLoanManager(address(cappedManager));
        cappedOracle.setLoanManager(address(cappedManager));
        cappedOracle.pinRoute(FACILITY, Route.Development);
        cappedOracle.setSigner(Route.Development, vm.addr(SIGNER_KEY), true);
        vm.stopPrank();

        token.mint(LENDER_A, 50_000e6);
        vm.startPrank(LENDER_A);
        token.approve(address(capped), 50_000e6);
        capped.deposit(50_000e6);
        vm.stopPrank();
        vm.prank(OPERATOR);
        capped.activate();

        CreditAccount acct =
            new CreditAccount(address(cappedManager), OWNER, AGENT, address(cappedOracle));
        token.mint(address(acct), 20_000e6);

        // Exactly the cap in principal. The interest it will owe pushes
        // the obligation past the budget, so this must be refused.
        Binding memory b = _publish(cappedOracle, acct, 10_000e6, 1_200, 10 days);
        vm.expectRevert(CreditVault.ExceedsGlobalCap.selector);
        cappedManager.originate(b, 10_000e6);
    }

    /// A loan that leaves room for its own interest still funds, and the
    /// reservation is released when it resolves.
    function test_ReservationIsReleasedOnRepayment() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        assertGt(vault.reservedObligation(), 0, "interest must be reserved while live");

        vm.warp(block.timestamp + 1 days);
        manager.repay(loanId);
        assertEq(vault.reservedObligation(), 0, "a resolved loan reserves nothing");
    }

    // --- Configuration is immutable for an obligation's life (docs/05) ---

    /// A facility's route cannot change while it owes.
    ///
    /// The borrower struck terms under one authorization domain. Re-pinning
    /// mid-loan would move it to another, which is a different instrument
    /// than the one it agreed to.
    function test_RouteCannotChangeUnderALiveLoan() public {
        _originate(account, 5_000e6, 1_200, 10 days);

        vm.prank(OPERATOR);
        vm.expectRevert(
            abi.encodeWithSelector(RiskOracle.FacilityHasLiveObligations.selector, FACILITY)
        );
        oracle.pinRoute(FACILITY, Route.CRE);
    }

    /// Once the obligation resolves, the epoch may be re-pinned.
    function test_RouteCanChangeOnceResolved() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 1 days);
        manager.repay(loanId);

        vm.prank(OPERATOR);
        oracle.pinRoute(FACILITY, Route.CRE);
        assertEq(uint8(oracle.facilityRoute(FACILITY)), uint8(Route.CRE));
    }

    /// Adding an execution route mid-loan changes what the agent may do
    /// with funds it already borrowed.
    function test_AdapterCannotBeAddedUnderALiveLoan() public {
        _originate(account, 5_000e6, 1_200, 10 days);

        vm.prank(OPERATOR);
        vm.expectRevert(LoanManager.AccountHasActiveLoan.selector);
        manager.setAccountAdapter(account, address(noop), true);
    }

    /// Revocation is always permitted: withdrawing a route only ever
    /// reduces what the account can do.
    function test_AdapterCanAlwaysBeRevoked() public {
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), true);
        _originate(account, 5_000e6, 1_200, 10 days);

        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), false);
        assertFalse(account.adapterAllowed(address(noop)));
    }

    // --- Recovery needs a reason (docs/05) ---

    /// A healthy, unmatured loan with standing permission cannot be
    /// seized. Otherwise every loan is callable at will.
    function test_RecoveryRefusedOnAHealthyUnmaturedLoan() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        // Grant standing permission, so neither trigger is present.
        Binding memory b = _bindingFor(account, account.stateVersion());
        OracleHarness.publishMonitoring(
            oracle, b, ++seq, SIGNER_KEY, true, DEPLOYMENT, uint64(block.timestamp)
        );

        // Inside the permission window and well short of maturity, so
        // neither trigger is present.
        vm.warp(block.timestamp + 10 minutes);
        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.LoanNotRecoverable.selector, loanId));
        manager.openRecovery(loanId, KEEPER, rb);
    }

    /// Maturity is a trigger: the obligation is due and unpaid.
    function test_RecoveryAllowedAtMaturity() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));
    }

    /// An unassessed loan becomes recoverable once the grace window has
    /// passed.
    ///
    /// **Renamed and corrected 14 September.** This test was called
    /// `test_RecoveryAllowedWhenPermissionWithdrawn` and its comment read
    /// "No standing permission at all, which reads as withdrawn" -- it
    /// asserted the defect as intended behavior. Absent permission is not
    /// a withdrawal, and reading it as one made a loan seizable in the
    /// block it was funded. What actually licenses recovery here is the
    /// day of silence, which is past `recoveryGraceSeconds`.
    ///
    /// The genuine withdrawal trigger is covered by
    /// `test_WithdrawnPermissionOpensRecoveryImmediately`, which needs no
    /// warp at all -- that is the difference.
    function test_RecoveryAllowedAfterGraceWithNoAssessment() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 1 days);

        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));
    }

    /// A fresh monitoring result must authorize the NEXT action, against
    /// the real oracle.
    ///
    /// `docs/05`: the binding's identity is immutable, its account state
    /// version is not. Committing to both together means a correctly
    /// signed monitoring assessment -- which necessarily carries the NEW
    /// version after an execution -- can never match what was pinned at
    /// origination, and the account is bricked after one action.
    function test_MonitoringAuthorizesTheActionAfterExecution() public {
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), true);

        uint256 principal = 5_000e6;
        Binding memory b = _publish(oracle, account, principal, 1_200, 10 days);
        manager.originate(b, principal);

        // The permitted action, taken once on the origination quote.
        bytes memory call_ = abi.encodeCall(NoopAdapter.act, ());
        vm.prank(AGENT);
        vm.expectRevert();
        account.execute(address(noop), call_);

        // A monitoring result for the account as it NOW stands.
        uint64 version = account.stateVersion();
        Binding memory fresh = _bindingFor(account, version);
        OracleHarness.publishMonitoring(
            oracle, fresh, ++seq, SIGNER_KEY, true, DEPLOYMENT, uint64(block.timestamp)
        );

        assertTrue(oracle.riskAllowed(fresh), "the oracle itself must consider this permitted");
        assertTrue(
            oracle.riskAllowedFor(
                oracle.bindingKey(fresh), oracle.bindingConfigHash(fresh), version
            ),
            "and the account's hash-keyed view must agree"
        );
    }

    /// Recovery eligibility must come from THIS loan's binding.
    ///
    /// An unrelated agent has no standing permission, so presenting one
    /// makes any healthy, unmatured loan look recoverable -- defeating the
    /// restriction on operator authority rather than respecting it.
    function test_UnrelatedBindingCannotOpenRecovery() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        // Grant real permission, so the loan is genuinely healthy.
        Binding memory own = _bindingFor(account, account.stateVersion());
        OracleHarness.publishMonitoring(
            oracle, own, ++seq, SIGNER_KEY, true, DEPLOYMENT, uint64(block.timestamp)
        );

        // A different agent, never assessed and therefore never permitted.
        Binding memory stranger = own;
        stranger.agentId = 999;

        vm.warp(block.timestamp + 10 minutes);
        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.BindingNotForLoan.selector, loanId));
        manager.openRecovery(loanId, KEEPER, stranger);
    }

    /// The loan's own binding still works, so the check bounds which
    /// binding may be used rather than blocking recovery.
    function test_OwnBindingStillOpensRecovery() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        Binding memory own = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, own);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));
    }

    /// An empty account receives no credit, whatever the quote says.
    ///
    /// `docs/02` states the rule as a property of v0, not of a particular
    /// attestor's output. A signed requiredEquity of zero would otherwise
    /// fund an account with no first loss layer at all.
    function test_EmptyAccountIsNotFunded() public {
        CreditAccount empty = new CreditAccount(address(manager), OWNER, AGENT, address(oracle));
        assertEq(token.balanceOf(address(empty)), 0, "the account must be empty");

        Binding memory b = _publish(oracle, empty, 5_000e6, 1_200, 10 days);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.InsufficientEquity.selector, 1, 0));
        manager.originate(b, 5_000e6);
    }

    // --- Identity transfer (docs/06) ---

    /// A registry transfer never moves or erases a loan.
    ///
    /// `docs/06`: on transfer, freeze new credit and require reassessment,
    /// while existing debt and recovery rights stay with the original
    /// record. The new identity must not inherit the obligation, and must
    /// not be able to act on the old one's authorization.
    /// Published feedback must be attributable (`docs/06`). Attribution
    /// that came from the publisher's own configuration would assert a
    /// link the chain never checked, so the loan records the agent from
    /// the binding its quote was consumed under.
    function test_LoanRecordsTheBindingsAgent() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        Binding memory b = _bindingFor(account, account.stateVersion());
        assertEq(
            manager.agentIdOf(loanId),
            b.agentId,
            "the loan must carry the binding's agent, not a supplied one"
        );

        // And it survives resolution: a publisher reads this AFTER the
        // obligation ends, when the binding is long gone.
        vm.warp(block.timestamp + 1 days);
        manager.repay(loanId);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Repaid), "loan resolved");
        assertEq(manager.agentIdOf(loanId), b.agentId, "attribution must outlive the obligation");
    }

    function test_IdentityTransferLeavesTheLoanIntact() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        assertEq(manager.activeLoan(address(account)), loanId, "the loan exists");

        // The same account, now claimed by a different agent id.
        Binding memory moved = _bindingFor(account, account.stateVersion());
        moved.agentId = 999;

        // The obligation is unchanged: it is keyed by the ACCOUNT, which
        // a registry transfer does not move.
        assertEq(manager.activeLoan(address(account)), loanId, "a transfer must not erase the loan");
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Active), "nor resolve it");

        // And the new identity cannot draw against it. The one-active-loan
        // rule stops it before the quote check even runs, which is the
        // stronger guarantee: a transfer cannot open a second obligation
        // on an account that already owes.
        vm.expectRevert(LoanManager.AccountHasActiveLoan.selector);
        manager.originate(moved, 5_000e6);

        // Once the obligation resolves, the new identity still has no
        // standing quote of its own -- new credit requires reassessment.
        vm.warp(block.timestamp + 1 days);
        manager.repay(loanId);
        vm.expectRevert(RiskOracle.NoQuote.selector);
        manager.originate(moved, 5_000e6);
    }

    /// Recovery rights stay with the loan, not the identity.
    ///
    /// A transferred agent must not be able to present its new binding to
    /// seize a position it did not originate.
    function test_TransferredIdentityCannotOpenRecovery() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        Binding memory moved = _bindingFor(account, account.stateVersion());
        moved.agentId = 999;

        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(LoanManager.BindingNotForLoan.selector, loanId));
        manager.openRecovery(loanId, KEEPER, moved);
    }

    /// The ORIGINAL identity can still repay: a transfer freezes new
    /// credit, it does not strand an existing obligation.
    function test_TransferDoesNotBlockRepayment() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 1 days);

        manager.repay(loanId);
        assertEq(
            uint8(manager.status(loanId)),
            uint8(LoanManager.Status.Repaid),
            "repayment must remain open"
        );
    }

    /// A fabricated state version cannot manufacture recovery eligibility.
    ///
    /// `bindingConfigHash` excludes the state version on purpose, so the
    /// binding check alone does not catch a wrong one — and a wrong one
    /// makes `riskAllowed` return false, which reads as "permission
    /// withdrawn" and liquidates a healthy, unmatured loan.
    function test_FabricatedStateVersionCannotOpenRecovery() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);

        // Genuinely permitted, so only the version is wrong.
        Binding memory own = _bindingFor(account, account.stateVersion());
        OracleHarness.publishMonitoring(
            oracle, own, ++seq, SIGNER_KEY, true, DEPLOYMENT, uint64(block.timestamp)
        );

        Binding memory forged = own;
        forged.accountStateVersion = own.accountStateVersion + 1;

        vm.warp(block.timestamp + 10 minutes);
        // Read before the prank: a call inside expectRevert consumes it.
        uint64 live = account.stateVersion();
        vm.prank(OPERATOR);
        vm.expectRevert(
            abi.encodeWithSelector(
                LoanManager.StaleAccountState.selector, forged.accountStateVersion, live
            )
        );
        manager.openRecovery(loanId, KEEPER, forged);
    }

    /// A real ERC-721 transfer invalidates a standing quote.
    ///
    /// An ERC-721 transfer PRESERVES the token id and changes the owner,
    /// so every binding field survives it unchanged — agent id included.
    /// A test that changes the agent id is testing a different thing
    /// entirely, and would pass while the actual transfer case stays
    /// open. `docs/06`: freeze new credit on transfer.
    address constant NEW_OWNER = address(0xBEEF);

    function test_TransferredIdentityCannotDrawAnOldQuote() public {
        MockIdentityRegistry registry = new MockIdentityRegistry();
        registry.mint(184, OPERATOR);
        vm.prank(OPERATOR);
        manager.setIdentityRegistry(address(registry));

        // A quote signed while OPERATOR owns the agent.
        Binding memory b = _publish(oracle, account, 5_000e6, 1_200, 10 days);
        assertEq(b.operator, OPERATOR, "the quote names its owner");

        // The identity is sold. The token id does not change.
        registry.transfer(184, NEW_OWNER);
        assertEq(registry.ownerOf(184), NEW_OWNER);

        vm.expectRevert(
            abi.encodeWithSelector(LoanManager.IdentityTransferred.selector, uint256(184))
        );
        manager.originate(b, 5_000e6);
    }

    /// Without a transfer the same quote still funds, so the check bounds
    /// who may draw rather than blocking origination.
    function test_UntransferredIdentityStillDraws() public {
        MockIdentityRegistry registry = new MockIdentityRegistry();
        registry.mint(184, OPERATOR);
        vm.prank(OPERATOR);
        manager.setIdentityRegistry(address(registry));

        Binding memory b = _publish(oracle, account, 5_000e6, 1_200, 10 days);
        uint256 loanId = manager.originate(b, 5_000e6);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Active));
    }
}
