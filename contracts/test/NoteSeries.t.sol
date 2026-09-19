// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {NoteSeries} from "../src/NoteSeries.sol";
import {TestSettlementToken} from "../src/TestSettlementToken.sol";

/// Notes as tradable debt exposure (`docs/08`).
///
/// The rules worth testing are the ones that would silently pay the wrong
/// person: a note is a claim on ACTUAL collections, not on face value, so
/// every arithmetic shortcut here has a holder on the other side of it.
contract NoteSeriesTest is Test {
    NoteSeries series;
    TestSettlementToken settlement;

    address constant OPERATOR = address(0xA1);
    address constant MANAGER = address(0xB2);
    address constant BORROWER = address(0xC3);
    address constant ALICE = address(0xD4);
    address constant BOB = address(0xE5);

    uint256 constant TARGET = 4_000e6;
    uint64 constant TERM = 10 days;
    uint32 constant APR_BPS = 1_200;

    function setUp() public {
        settlement = new TestSettlementToken();
        series = new NoteSeries(address(settlement), OPERATOR, BORROWER, TARGET, TERM, APR_BPS, 6);
        vm.prank(OPERATOR);
        series.setLoanManager(MANAGER);

        settlement.mint(ALICE, TARGET);
        settlement.mint(BOB, TARGET);
        settlement.mint(BORROWER, TARGET); // for repayments
    }

    function _subscribe(address who, uint256 amount) internal {
        vm.startPrank(who);
        settlement.approve(address(series), amount);
        series.subscribe(amount);
        vm.stopPrank();
    }

    /// Fund the series fully and draw the whole principal.
    function _fundAndLend() internal {
        _subscribe(ALICE, TARGET);
        vm.prank(OPERATOR);
        series.activate();
        vm.prank(MANAGER);
        series.lend(BORROWER, TARGET, 13e6);
    }

    /// The borrower returns principal plus `interest`, and the manager
    /// records it the way `LoanManager.repay` does: transfer, then record.
    function _repay(uint256 principal, uint256 interest) internal {
        vm.prank(BORROWER);
        settlement.transfer(address(series), principal + interest);
        vm.startPrank(MANAGER);
        series.recordPrincipalRepaid(BORROWER, principal);
        series.recordInterestCollected(BORROWER, interest);
        series.releaseReservation(13e6);
        vm.stopPrank();
    }

    // --- Funding ---

    function test_subscriptionIssuesNotesOneForOne() public {
        _subscribe(ALICE, 1_000e6);
        assertEq(series.balanceOf(ALICE), 1_000e6);
        assertEq(series.totalSupply(), 1_000e6);
        assertEq(series.idleCash(), 1_000e6);
    }

    /// `docs/08`: fixed supply issued at origination. Oversubscription would
    /// issue notes against principal the borrower never draws.
    function test_cannotSubscribeBeyondTarget() public {
        _subscribe(ALICE, TARGET);
        vm.startPrank(BOB);
        settlement.approve(address(series), 1);
        vm.expectRevert(NoteSeries.ExceedsTarget.selector);
        series.subscribe(1);
        vm.stopPrank();
    }

    /// A partially funded series would lend less than its notes claim, and
    /// the shortfall would surface at redemption as a loss nobody caused.
    function test_cannotActivateUnderSubscribed() public {
        _subscribe(ALICE, TARGET - 1);
        vm.prank(OPERATOR);
        vm.expectRevert(
            abi.encodeWithSelector(NoteSeries.UnderSubscribed.selector, TARGET - 1, TARGET)
        );
        series.activate();
    }

    // --- The funding-source interface ---

    /// A series exists for ONE loan. Lending elsewhere would fund a loan its
    /// holders never bought.
    function test_lendOnlyToTheSeriesBorrower() public {
        _subscribe(ALICE, TARGET);
        vm.prank(OPERATOR);
        series.activate();

        vm.prank(MANAGER);
        vm.expectRevert(NoteSeries.NotThisBorrower.selector);
        series.lend(BOB, TARGET, 0);
    }

    function test_onlyTheManagerCanLend() public {
        _subscribe(ALICE, TARGET);
        vm.prank(OPERATOR);
        series.activate();

        vm.prank(OPERATOR);
        vm.expectRevert(NoteSeries.NotLoanManager.selector);
        series.lend(BORROWER, TARGET, 0);
    }

    /// The manager is set once. A repointable manager could move an
    /// obligation holders already own.
    function test_managerCannotBeRepointed() public {
        vm.prank(OPERATOR);
        vm.expectRevert(NoteSeries.ManagerAlreadySet.selector);
        series.setLoanManager(address(0xF6));
    }

    // --- Redemption: the arithmetic holders live with ---

    /// A clean repayment returns principal plus the interest COLLECTED.
    function test_repaidSeriesRedeemsAboveFaceValue() public {
        _fundAndLend();
        _repay(TARGET, 6_575_343);

        vm.prank(OPERATOR);
        series.openRedemption();

        vm.prank(ALICE);
        uint256 got = series.redeem();

        assertEq(got, TARGET + 6_575_343, "holder receives principal plus collected interest");
        assertEq(series.balanceOf(ALICE), 0);
    }

    /// `docs/08`: "Default can redeem below principal." The whole point of
    /// the instrument -- a note is a claim on collections, not a promise.
    function test_defaultedSeriesRedeemsBelowFaceValue() public {
        _fundAndLend();

        // Only 1,400 of 4,000 comes back; the rest is written off.
        vm.prank(BORROWER);
        settlement.transfer(address(series), 1_400e6);
        vm.startPrank(MANAGER);
        series.recordPrincipalRepaid(BORROWER, 1_400e6);
        series.recordLoss(BORROWER, 2_600e6);
        series.releaseReservation(13e6);
        vm.stopPrank();

        vm.prank(OPERATOR);
        series.openRedemption();

        vm.prank(ALICE);
        uint256 got = series.redeem();

        assertEq(got, 1_400e6, "redeems what was collected, not face value");
        assertEq(series.realizedLoss(), 2_600e6);
        assertLt(got, TARGET, "a defaulted note must redeem below face");
    }

    /// `docs/08`: "Early repayment pays accrued interest only -- the note
    /// does not promise unearned full-term interest."
    function test_earlyRepaymentPaysOnlyAccruedInterest() public {
        _fundAndLend();

        // Repaid after one day of a ten-day term: a tenth of the interest.
        uint256 fullTerm = 13e6;
        uint256 accrued = fullTerm / 10;
        _repay(TARGET, accrued);

        vm.prank(OPERATOR);
        series.openRedemption();
        vm.prank(ALICE);
        uint256 got = series.redeem();

        assertEq(got, TARGET + accrued);
        assertLt(got, TARGET + fullTerm, "unearned interest is never distributed");
        assertEq(series.reservedObligation(), 0, "the reservation is released, not paid");
    }

    /// `docs/08`: "pending recovery means pending redemption." Paying the
    /// first holder from an incomplete pot spends a later holder's share.
    function test_redemptionBlockedWhileRecoveryPends() public {
        _fundAndLend();

        // Principal is BACK -- only an unresolved receivable remains, so
        // this isolates the receivables guard. An earlier version left
        // principal outstanding too, and the test passed with the
        // receivables check deleted because the other guard caught it.
        _repay(TARGET, 0);
        vm.prank(MANAGER);
        series.markReceivableUnresolved(6_575_343);

        assertEq(series.outstandingPrincipal(), 0, "principal is settled");

        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(NoteSeries.RecoveryPending.selector, 6_575_343));
        series.openRedemption();
    }

    /// Principal still outstanding is the same hazard by another name.
    function test_redemptionBlockedWhilePrincipalOutstanding() public {
        _fundAndLend();

        vm.prank(OPERATOR);
        vm.expectRevert(abi.encodeWithSelector(NoteSeries.RecoveryPending.selector, TARGET));
        series.openRedemption();
    }

    // --- Transfer: what makes the claim tradable ---

    /// The holder at redemption is paid, not the original subscriber.
    function test_transferMovesTheClaim() public {
        _fundAndLend();
        _repay(TARGET, 6_575_343);

        vm.prank(ALICE);
        series.transfer(BOB, TARGET);

        vm.prank(OPERATOR);
        series.openRedemption();

        vm.prank(BOB);
        uint256 got = series.redeem();
        assertEq(got, TARGET + 6_575_343, "the buyer redeems, not the seller");

        vm.prank(ALICE);
        vm.expectRevert(NoteSeries.NothingToRedeem.selector);
        series.redeem();
    }

    /// A partial sale splits the claim pro rata, and the two halves sum to
    /// the whole. This is where a rounding shortcut would quietly short one
    /// of them.
    function test_partialTransferSplitsTheClaimProRata() public {
        _fundAndLend();
        _repay(TARGET, 6_575_343);

        vm.prank(ALICE);
        series.transfer(BOB, TARGET / 4);

        vm.prank(OPERATOR);
        series.openRedemption();

        vm.prank(BOB);
        uint256 bobGot = series.redeem();
        vm.prank(ALICE);
        uint256 aliceGot = series.redeem();

        assertEq(
            bobGot + aliceGot,
            TARGET + 6_575_343,
            "the two claims sum to the whole pot, with no dust stranded"
        );
        assertEq(series.idleCash(), 0, "nothing is left behind in the contract");
    }

    /// Truncation accumulates to the LAST redeemer rather than stranding
    /// dust, matching `CreditVault.claim` (`docs/05`).
    function test_dustGoesToTheFinalRedeemer() public {
        // Three holders and a pot that does not divide evenly.
        _subscribe(ALICE, TARGET);
        vm.prank(OPERATOR);
        series.activate();
        vm.prank(MANAGER);
        series.lend(BORROWER, TARGET, 0);

        vm.prank(ALICE);
        series.transfer(BOB, TARGET / 3);

        // One base unit of interest makes the pot indivisible by three.
        _repayNoReservation(TARGET, 1);

        vm.prank(OPERATOR);
        series.openRedemption();

        vm.prank(BOB);
        uint256 bobGot = series.redeem();
        vm.prank(ALICE);
        uint256 aliceGot = series.redeem();

        assertEq(bobGot + aliceGot, TARGET + 1, "every base unit is distributed");
        assertEq(series.idleCash(), 0);
    }

    function _repayNoReservation(uint256 principal, uint256 interest) internal {
        vm.prank(BORROWER);
        settlement.transfer(address(series), principal + interest);
        vm.startPrank(MANAGER);
        series.recordPrincipalRepaid(BORROWER, principal);
        series.recordInterestCollected(BORROWER, interest);
        vm.stopPrank();
    }

    /// A holder who wants out after the outcome is known should be able to
    /// sell rather than being forced to redeem. That secondary price is the
    /// signal `docs/08` keeps INFORMATIONAL.
    function test_notesRemainTransferableAfterRedemptionOpens() public {
        _fundAndLend();
        _repay(TARGET, 6_575_343);
        vm.prank(OPERATOR);
        series.openRedemption();

        vm.prank(ALICE);
        series.transfer(BOB, TARGET);
        assertEq(series.balanceOf(BOB), TARGET);

        vm.prank(BOB);
        assertEq(series.redeem(), TARGET + 6_575_343);
    }

    // --- Quoting ---

    /// Quoting a per-note value from an incomplete collection would be a
    /// marked estimate dressed as a settlement.
    function test_noRedemptionQuoteBeforeTheOutcomeIsKnown() public {
        _fundAndLend();
        assertEq(series.redemptionPerNote(), 0, "no quote while the loan runs");

        _repay(TARGET, 6_575_343);
        vm.prank(OPERATOR);
        series.openRedemption();

        assertGt(series.redemptionPerNote(), 0, "quoted once the pot is final");
    }
}
