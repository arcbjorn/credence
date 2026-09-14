// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {
    MockIdentityRegistry, MockReputationRegistry
} from "../src/MockERC8004.sol";

/// A test double is only useful if it fails where the real contract
/// fails. These pin the three behaviours Credence's design depends on,
/// each verified against the deployed registry on Monad testnet
/// (chain 10143) on 14 September.
contract MockERC8004Test is Test {
    MockIdentityRegistry identity;
    MockReputationRegistry reputation;

    address constant AGENT_OWNER = address(0xA9E7);
    address constant CLIENT = address(0xC11E);
    uint256 constant AGENT_ID = 1830;

    function setUp() public {
        identity = new MockIdentityRegistry();
        reputation = new MockReputationRegistry(identity);
        identity.registerTo(AGENT_OWNER, AGENT_ID);
    }

    /// The selectors must equal the deployed registry's, or a call that
    /// passes here lands nowhere against the real one.
    function test_selectorsMatchTheDeployedRegistry() public pure {
        assertEq(
            bytes4(keccak256(
                "giveFeedback(uint256,int128,uint8,string,string,string,string,bytes32)"
            )),
            bytes4(0x3c036a7e),
            "giveFeedback selector"
        );
        assertEq(
            bytes4(keccak256(
                "readAllFeedback(uint256,address[],string,string,bool)"
            )),
            bytes4(0xd9d84224),
            "readAllFeedback selector"
        );
    }

    /// Observed on chain: an agent's owner cannot report on itself. This
    /// is why Credence publishes as a separate client rather than owning
    /// the agent it reports on.
    function test_selfFeedbackIsRefused() public {
        vm.prank(AGENT_OWNER);
        vm.expectRevert("Self-feedback not allowed");
        reputation.giveFeedback(
            AGENT_ID, 1, 0, "credence.interest", "loan:1", "", "", bytes32(0));
    }

    /// The registry does NOT dedupe. Sending the same outcome twice
    /// yields two entries, so idempotency has to happen before the send.
    function test_duplicateSendsAreNotRefused() public {
        vm.startPrank(CLIENT);
        reputation.giveFeedback(
            AGENT_ID, 1, 0, "credence.interest", "loan:1", "", "", bytes32(0));
        reputation.giveFeedback(
            AGENT_ID, 1, 0, "credence.interest", "loan:1", "", "", bytes32(0));
        vm.stopPrank();

        (, uint64[] memory indexes,,,,,) = reputation.readAllFeedback(
            AGENT_ID, new address[](0), "", "", true);
        assertEq(indexes.length, 2, "the registry kept both");
        assertEq(indexes[0], 1, "1-indexed");
        assertEq(indexes[1], 2, "and auto-incrementing");
    }

    /// A loss must survive the round trip as a negative number. Reading
    /// it unsigned would report a default as an enormous gain.
    function test_negativeValueRoundTrips() public {
        vm.prank(CLIENT);
        reputation.giveFeedback(
            AGENT_ID, -2_600_000_000, 6, "credence.loss", "loan:1",
            "", "", bytes32(0));

        (,, int128[] memory values,,,,) = reputation.readAllFeedback(
            AGENT_ID, new address[](0), "", "", true);
        assertEq(values[0], -2_600_000_000, "loss stays negative");
    }

    /// Tag filtering is what lets a reader select one signal without
    /// interpreting the others.
    function test_tagsFilter() public {
        vm.startPrank(CLIENT);
        reputation.giveFeedback(
            AGENT_ID, 4_000_000_000, 6, "credence.outcome.repaid", "loan:1",
            "", "", bytes32(0));
        reputation.giveFeedback(
            AGENT_ID, 0, 6, "credence.loss", "loan:1", "", "", bytes32(0));
        reputation.giveFeedback(
            AGENT_ID, 7, 6, "credence.loss", "loan:2", "", "", bytes32(0));
        vm.stopPrank();

        (,, int128[] memory losses,,,,) = reputation.readAllFeedback(
            AGENT_ID, new address[](0), "credence.loss", "", true);
        assertEq(losses.length, 2, "tag1 selects both losses");

        (,, int128[] memory loan2,,,,) = reputation.readAllFeedback(
            AGENT_ID, new address[](0), "credence.loss", "loan:2", true);
        assertEq(loan2.length, 1, "tag1+tag2 selects one obligation");
        assertEq(loan2[0], 7, "and the right one");
    }

    /// An unregistered agent has no owner, so feedback about it attributes
    /// an outcome to nobody.
    function test_unregisteredAgentReverts() public {
        vm.prank(CLIENT);
        vm.expectRevert(
            abi.encodeWithSelector(
                MockIdentityRegistry.ERC721NonexistentToken.selector, uint256(999))
        );
        reputation.giveFeedback(
            999, 1, 0, "credence.interest", "loan:1", "", "", bytes32(0));
    }
}
