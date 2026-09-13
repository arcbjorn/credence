// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {Assessment, Binding, Class, Decision, Kind, Permission, Route, Terms} from "../src/Assessment.sol";
import {IERC20} from "../src/IERC20.sol";
import {MockERC20} from "./MockERC20.sol";
import {OracleHarness} from "./OracleHarness.sol";

/// Minimal adapter used to advance an account's state version.
contract NoopAdapter {
    function act() external {}
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
        oracle = new RiskOracle(OPERATOR, DEPLOYMENT);
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
        b = OracleHarness.binding(address(acct), acct.stateVersion(), FACILITY);
        Assessment memory a = OracleHarness.approvedQuote(
            b, ++seq, principal, aprBps, term, 0, address(token), DEPLOYMENT,
            uint64(block.timestamp)
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
        Binding memory b =
            OracleHarness.binding(address(acct), acct.stateVersion(), FACILITY);
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
        vm.expectRevert(
            abi.encodeWithSelector(CreditVault.ReceivablesUnresolved.selector, 5_000e6)
        );
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
        RiskOracle cappedOracle = new RiskOracle(OPERATOR, DEPLOYMENT);
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
        Binding memory b = _publish(cappedOracle, acct, 10_001e6, 1_200, 10 days);
        vm.expectRevert(CreditVault.ExceedsGlobalCap.selector);
        cappedManager.originate(b, 10_001e6);
    }

    // --- Authorization is load-bearing ---

    /// Without a standing quote there is no loan. The operator cannot
    /// originate on trust alone.
    function test_originationRequiresQuote() public {
        Binding memory b =
            OracleHarness.binding(address(account), account.stateVersion(), FACILITY);
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

    /// The binding must describe the account as it stands. If the account
    /// executed after assessment, the assessed portfolio no longer exists.
    function test_staleAccountStateRejected() public {
        Binding memory b = _publish(oracle, account, 1_000e6, 1_200, 10 days);

        // Account acts between assessment and draw, the way it really would.
        vm.prank(OPERATOR);
        manager.setAccountAdapter(account, address(noop), true);
        vm.prank(AGENT);
        account.execute(address(noop), abi.encodeCall(NoopAdapter.act, ()));

        vm.expectRevert(
            abi.encodeWithSelector(LoanManager.StaleAccountState.selector, 0, 1)
        );
        manager.originate(b, 1_000e6);
    }

    /// `docs/02`: an agent with no equity receives no credit in v0.
    function test_insufficientEquityRejected() public {
        CreditAccount poor = new CreditAccount(address(manager), OWNER, AGENT, address(oracle));
        Binding memory b =
            OracleHarness.binding(address(poor), poor.stateVersion(), FACILITY);
        Assessment memory a = OracleHarness.approvedQuote(
            b, ++seq, 1_000e6, 1_200, 10 days, 5_000e6, address(token), DEPLOYMENT,
            uint64(block.timestamp)
        );
        oracle.accept(a, OracleHarness.sign(oracle, a, SIGNER_KEY));

        vm.expectRevert(
            abi.encodeWithSelector(LoanManager.InsufficientEquity.selector, 5_000e6, 0)
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
        assertGt(held, owed, "the account must hold more than it owes for this test to mean anything");

        // Ask for everything; only the obligation may move.
        vm.prank(OPERATOR);
        manager.collect(loanId, held);

        assertEq(
            token.balanceOf(address(account)), held - owed,
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
        RiskOracle cappedOracle = new RiskOracle(OPERATOR, DEPLOYMENT);
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
        vm.expectRevert(
            abi.encodeWithSelector(LoanManager.LoanNotRecoverable.selector, loanId)
        );
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

    /// Withdrawn permission is the other trigger: the engine assessed the
    /// position and blocked it, which is this protocol's covenant breach.
    function test_RecoveryAllowedWhenPermissionWithdrawn() public {
        uint256 loanId = _originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 1 days);

        // No standing permission at all, which reads as withdrawn.
        Binding memory rb = _bindingFor(account, account.stateVersion());
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER, rb);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));
    }
}
