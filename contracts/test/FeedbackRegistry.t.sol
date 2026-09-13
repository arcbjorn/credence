// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {FeedbackRegistry} from "../src/FeedbackRegistry.sol";

contract FeedbackRegistryTest is Test {
    FeedbackRegistry registry;

    address constant PUBLISHER = address(0xA1);
    address constant ACCOUNT = address(0xB2);
    uint256 constant AGENT = 184;

    function setUp() public {
        registry = new FeedbackRegistry(PUBLISHER);
    }

    function _entry(uint256 loanId, FeedbackRegistry.Outcome outcome)
        internal
        pure
        returns (FeedbackRegistry.Feedback memory)
    {
        return FeedbackRegistry.Feedback({
            agentId: AGENT,
            creditAccount: ACCOUNT,
            loanId: loanId,
            outcome: outcome,
            principal: 4_000e6,
            interestPaid: outcome == FeedbackRegistry.Outcome.Repaid ? 6_575_343 : 0,
            principalLoss: outcome == FeedbackRegistry.Outcome.Defaulted ? 3_100e6 : 0,
            originatedAt: 1_000,
            resolvedAt: 433_000,
            evidenceRef: keccak256(abi.encode("evidence", loanId))
        });
    }

    /// A queue that retries must not double-count an outcome (`docs/06`).
    function test_PublishingTheSameLoanTwiceIsRefused() public {
        vm.startPrank(PUBLISHER);
        registry.publish(_entry(1, FeedbackRegistry.Outcome.Repaid));
        vm.expectRevert(
            abi.encodeWithSelector(FeedbackRegistry.AlreadyPublished.selector, uint256(1))
        );
        registry.publish(_entry(1, FeedbackRegistry.Outcome.Repaid));
        vm.stopPrank();

        assertEq(registry.entryCount(), 1, "one loan, one entry");
    }

    /// Feedback is attributable: only the publisher may write it, or the
    /// history means nothing.
    function test_OnlyPublisherMayPublish() public {
        vm.prank(address(0xBAD));
        vm.expectRevert(FeedbackRegistry.NotPublisher.selector);
        registry.publish(_entry(1, FeedbackRegistry.Outcome.Repaid));
    }

    /// The record is append-only. A correction is a new entry, never a
    /// rewrite of an old one (`docs/06`).
    function test_HistoryIsAppendOnly() public {
        vm.startPrank(PUBLISHER);
        registry.publish(_entry(1, FeedbackRegistry.Outcome.Repaid));
        registry.publish(_entry(2, FeedbackRegistry.Outcome.Defaulted));
        vm.stopPrank();

        uint256[] memory indices = registry.agentEntries(AGENT);
        assertEq(indices.length, 2);

        // The first entry is untouched by the second.
        FeedbackRegistry.Feedback memory first = registry.entryAt(indices[0]);
        assertEq(first.loanId, 1);
        assertEq(uint8(first.outcome), uint8(FeedbackRegistry.Outcome.Repaid));
        assertEq(first.principalLoss, 0);
    }

    /// Outcomes stay separate. A default and a repayment are different
    /// facts, and the registry must not blend them into a score.
    function test_SignalsAreRecordedSeparately() public {
        vm.startPrank(PUBLISHER);
        registry.publish(_entry(1, FeedbackRegistry.Outcome.Repaid));
        registry.publish(_entry(2, FeedbackRegistry.Outcome.Defaulted));
        vm.stopPrank();

        FeedbackRegistry.Feedback memory repaid = registry.entryAt(0);
        FeedbackRegistry.Feedback memory defaulted = registry.entryAt(1);

        assertGt(repaid.interestPaid, 0, "a repayment records interest");
        assertEq(repaid.principalLoss, 0, "and no loss");
        assertEq(defaulted.interestPaid, 0, "a default records no interest");
        assertGt(defaulted.principalLoss, 0, "and a loss");
    }

    /// A reader must be able to enumerate one agent without scanning the
    /// whole registry.
    function test_EntriesAreIndexedByAgent() public {
        vm.startPrank(PUBLISHER);
        registry.publish(_entry(1, FeedbackRegistry.Outcome.Repaid));

        FeedbackRegistry.Feedback memory other = _entry(2, FeedbackRegistry.Outcome.Repaid);
        other.agentId = 999;
        registry.publish(other);
        vm.stopPrank();

        assertEq(registry.agentEntries(AGENT).length, 1, "only this agent's entry");
        assertEq(registry.agentEntries(999).length, 1, "and only the other's");
    }
}
