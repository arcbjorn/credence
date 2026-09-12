// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {IERC20} from "../src/IERC20.sol";
import {MockERC20} from "./MockERC20.sol";

contract LifecycleTest is Test {
    MockERC20 token;
    CreditVault vault;
    LoanManager manager;
    CreditAccount account;

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
        manager = new LoanManager(IERC20(address(token)), vault, OPERATOR);

        vm.prank(OPERATOR);
        vault.setLoanManager(address(manager));

        account = new CreditAccount(address(manager), OWNER, AGENT);

        _fund(LENDER_A, 60_000e6);
        _fund(LENDER_B, 40_000e6);
        vm.prank(OPERATOR);
        vault.activate();

        // Agent equity, per docs/02: no equity, no credit in v0.
        token.mint(address(account), 5_000e6);
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
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);

        vm.warp(block.timestamp + 10 days);
        // risk-core: full_term(5_000_000_000, 1200, 864000) == 16_438_357
        assertEq(manager.accruedInterest(loanId), 16_438_357);
        assertEq(manager.debtDue(loanId), 5_016_438_357);
    }

    /// docs/03: accrual stops at maturity. No late penalty, no compounding.
    function test_accrualStopsAtMaturity() public {
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);

        vm.warp(block.timestamp + 10 days);
        uint256 atMaturity = manager.accruedInterest(loanId);
        vm.warp(block.timestamp + 100 days);
        assertEq(manager.accruedInterest(loanId), atMaturity);
    }

    // --- Happy path ---

    function test_repaidPathConservesFunds() public {
        uint256 before = token.balanceOf(address(vault));

        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);
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
        vm.startPrank(OPERATOR);
        manager.originate(account, 1_000e6, 1_200, 10 days);
        vm.expectRevert(LoanManager.AccountHasActiveLoan.selector);
        manager.originate(account, 1_000e6, 1_200, 10 days);
        vm.stopPrank();
    }

    /// docs/05: a pause halts originations but never blocks repayment.
    function test_pauseBlocksOriginationNotRepayment() public {
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 1_000e6, 1_200, 10 days);

        vm.prank(OPERATOR);
        manager.setPaused(true);

        vm.prank(OPERATOR);
        vm.expectRevert(LoanManager.Paused.selector);
        manager.originate(account, 1_000e6, 1_200, 10 days);

        manager.repay(loanId); // still open
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Repaid));
    }

    // --- Recovery and loss ---

    /// docs/05: collection accounting is idempotent across several calls.
    function test_recoveryCollectsInStages() public {
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER);
        assertEq(vault.unresolvedReceivables(), 5_000e6);

        manager.collect(loanId, 2_000e6);
        assertEq(vault.unresolvedReceivables(), 3_000e6);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.RecoveryPending));

        uint256 rest = manager.debtDue(loanId);
        manager.collect(loanId, rest);
        assertEq(uint8(manager.status(loanId)), uint8(LoanManager.Status.Recovered));
        assertEq(vault.unresolvedReceivables(), 0);
    }

    /// docs/02 worked loss path: the shortfall lands on lenders, and
    /// principal loss is recorded separately from unpaid interest.
    function test_defaultAllocatesLossToVault() public {
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER);

        // Shock: only 3,940 recoverable against 5,016.44 of debt.
        uint256 accountBalance = token.balanceOf(address(account));
        vm.prank(address(manager));
        account.sweepTo(address(token), address(0xDEAD), accountBalance - 3_940e6);

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
        vm.prank(OPERATOR);
        vm.expectRevert(LoanManager.AccountInDefault.selector);
        manager.originate(account, 1_000e6, 1_200, 10 days);
    }

    /// docs/05: unavailable liquidity leaves the loan RecoveryPending. It is
    /// never resolved because a timeout elapsed.
    function test_cannotDefaultWhileAssetsRemain() public {
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);

        vm.startPrank(OPERATOR);
        manager.openRecovery(loanId, KEEPER);
        vm.expectRevert(LoanManager.NotYetDefaultable.selector);
        manager.declareDefault(loanId);
        vm.stopPrank();
    }

    // --- Vault epoch and settlement ---

    /// docs/05: the epoch cannot settle while a receivable is unresolved.
    function test_settleBlockedWhileReceivableOpen() public {
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);

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
        LoanManager cappedManager = new LoanManager(IERC20(address(token)), capped, OPERATOR);
        vm.prank(OPERATOR);
        capped.setLoanManager(address(cappedManager));

        token.mint(LENDER_A, 50_000e6);
        vm.startPrank(LENDER_A);
        token.approve(address(capped), 50_000e6);
        capped.deposit(50_000e6);
        vm.stopPrank();
        vm.prank(OPERATOR);
        capped.activate();

        CreditAccount acct = new CreditAccount(address(cappedManager), OWNER, AGENT);
        vm.prank(OPERATOR);
        vm.expectRevert(CreditVault.ExceedsGlobalCap.selector);
        cappedManager.originate(acct, 10_001e6, 1_200, 10 days);
    }

    function _driveToDefault() private {
        vm.prank(OPERATOR);
        uint256 loanId = manager.originate(account, 5_000e6, 1_200, 10 days);
        vm.warp(block.timestamp + 10 days);
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, KEEPER);

        uint256 bal = token.balanceOf(address(account));
        vm.prank(address(manager));
        account.sweepTo(address(token), address(0xDEAD), bal - 3_940e6);
        manager.collect(loanId, 3_940e6);

        vm.prank(OPERATOR);
        manager.declareDefault(loanId);
    }
}
