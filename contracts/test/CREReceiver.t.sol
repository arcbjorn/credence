// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CREReceiver, IReceiver} from "../src/CREReceiver.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {
    Assessment, Binding, Terms, Route, Kind, Decision, Permission, Class
} from "../src/Assessment.sol";

/// Stands in for the KeystoneForwarder, which only needs to be an address
/// the receiver trusts. The real forwarder's job -- DON signature
/// verification -- happens before it ever calls a consumer, so reproducing
/// it here would test Chainlink's contract rather than ours.
contract StubForwarder {
    function deliver(address receiver, bytes calldata metadata, bytes calldata report)
        external
    {
        IReceiver(receiver).onReport(metadata, report);
    }
}

contract CREReceiverTest is Test {
    address constant OPERATOR = address(0x09E3);
    address constant CONSUMER = address(0xC0);
    uint256 constant DEPLOYMENT = 7;
    uint256 constant FACILITY = 1;

    bytes32 constant WORKFLOW_ID = bytes32(uint256(0xC0DE));
    bytes10 constant WORKFLOW_NAME = bytes10("credence");
    address constant WORKFLOW_OWNER = address(0x0BEE);

    RiskOracle oracle;
    CREReceiver receiver;
    StubForwarder forwarder;

    function setUp() public {
        oracle = new RiskOracle(OPERATOR, DEPLOYMENT, Class.PrototypeEligible);
        forwarder = new StubForwarder();
        receiver = new CREReceiver(
            address(forwarder), WORKFLOW_OWNER, WORKFLOW_ID, oracle);

        vm.startPrank(OPERATOR);
        oracle.setLoanManager(CONSUMER);
        oracle.pinRoute(FACILITY, Route.CRE);
        // The receiver IS the CRE route's authority: a report carries no
        // ECDSA signature, so the caller is what the oracle trusts.
        oracle.setSigner(Route.CRE, address(receiver), true);
        vm.stopPrank();

        vm.warp(1_000_000);
    }

    // --- helpers ---

    /// Metadata exactly as the live forwarder delivers it.
    ///
    /// Verified on Monad testnet, 14 September, by routing a report through
    /// the deployed mock forwarder (0xB9F79d86...) to a probe that recorded
    /// what arrived: 64 bytes, being the 62-byte packed identity
    /// word-aligned, with the report id in the trailing two.
    function _metadata(bytes32 id, address owner, bytes2 reportId)
        internal
        pure
        returns (bytes memory)
    {
        return abi.encodePacked(id, WORKFLOW_NAME, owner, reportId);
    }

    function _assessment(uint64 seq) internal view returns (Assessment memory a) {
        a = Assessment({
            deploymentId: DEPLOYMENT,
            evidencePolicyId: keccak256("evidence-v1"),
            route: Route.CRE,
            binding: Binding({
                identityRegistryChainId: 10143,
                identityRegistry: address(0xAEC0),
                agentId: 1830,
                operator: address(0xA9E7),
                creditAccount: address(0xACC7),
                facilityId: FACILITY,
                accountStateVersion: 0,
                configVersion: 1,
                configHash: keccak256("config"),
                policyEpoch: 1,
                policyHash: keccak256("policy")
            }),
            kind: Kind.Monitoring,
            assessmentId: keccak256(abi.encode("assessment", seq)),
            assessmentHash: keccak256("output"),
            decision: Decision.Approved,
            permission: Permission.Allowed,
            modelHash: keccak256("model-v1"),
            inputHash: keccak256("input"),
            evidenceRoot: keccak256("evidence"),
            class_: Class.PrototypeEligible,
            sequence: seq,
            validAfter: uint64(block.timestamp),
            validUntil: uint64(block.timestamp + 1 hours),
            requestId: bytes32(0),
            intentHash: bytes32(0),
            settlementAsset: address(0x5E77),
            requestedPrincipal: 0,
            loanId: 1,
            terms: Terms({
                approvedPrincipal: 0,
                aprBps: 0,
                termSeconds: 0,
                requiredEquity: 0,
                permittedActionHash: bytes32(0)
            })
        });
    }

    function _deliver(Assessment memory a, bytes2 reportId) internal {
        forwarder.deliver(
            address(receiver),
            _metadata(WORKFLOW_ID, WORKFLOW_OWNER, reportId),
            abi.encode(a)
        );
    }

    // --- the happy path ---

    /// A well-formed report from the expected forwarder and workflow
    /// reaches the oracle and updates permission, with no signature
    /// anywhere in the path.
    function test_validReportUpdatesPermission() public {
        Assessment memory a = _assessment(1);
        _deliver(a, 0x0001);

        bytes32 key = oracle.bindingKey(a.binding);
        assertEq(oracle.lastSequence(key), 1, "the oracle recorded it");
        assertTrue(
            oracle.riskAllowedFor(
                key, oracle.bindingConfigHash(a.binding),
                a.binding.accountStateVersion),
            "permission stands"
        );
    }

    // --- check 1: the forwarder ---

    /// Without this the DON is decorative: anyone calls onReport directly
    /// and authors their own assessment.
    function test_onlyTheForwarderMayDeliver() public {
        Assessment memory a = _assessment(1);
        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.UnexpectedSender.selector,
                address(this), address(forwarder))
        );
        receiver.onReport(
            _metadata(WORKFLOW_ID, WORKFLOW_OWNER, 0x0001), abi.encode(a));
    }

    // --- check 2: the workflow owner ---

    /// One forwarder routes for every workflow on the chain, so it attests
    /// that SOME workflow produced this, never whose.
    function test_foreignWorkflowOwnerIsRefused() public {
        Assessment memory a = _assessment(1);
        address intruder = address(0xBAD);
        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.UnexpectedWorkflowOwner.selector,
                intruder, WORKFLOW_OWNER)
        );
        forwarder.deliver(
            address(receiver),
            _metadata(WORKFLOW_ID, intruder, 0x0001),
            abi.encode(a)
        );
    }

    // --- check 3: the workflow id ---

    /// An owner may run several workflows; only one underwrites. A
    /// different workflow of the same owner is not an underwriting result.
    function test_foreignWorkflowIdIsRefused() public {
        Assessment memory a = _assessment(1);
        bytes32 other = bytes32(uint256(0xDEAD));
        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.UnexpectedWorkflow.selector, other, WORKFLOW_ID)
        );
        forwarder.deliver(
            address(receiver),
            _metadata(other, WORKFLOW_OWNER, 0x0001),
            abi.encode(a)
        );
    }

    // --- check 4: the route ---

    /// `docs/05` invariant 6. A report claiming the development route is a
    /// malformed message, not a fallback.
    function test_developmentRouteInAReportIsRefused() public {
        Assessment memory a = _assessment(1);
        a.route = Route.Development;
        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.NotCRERoute.selector, Route.Development)
        );
        _deliver(a, 0x0001);
    }

    // --- check 5: replay ---

    /// The forwarder retries a reverted delivery with more gas, so a
    /// receiver that acted twice would let one DON round advance the
    /// sequence twice (`docs/05` invariant 2).
    function test_sameReportCannotBeDeliveredTwice() public {
        Assessment memory a = _assessment(1);
        _deliver(a, 0x0001);

        vm.expectRevert();
        _deliver(a, 0x0001);
    }

    /// But a genuinely new report with a reused two-byte id must still get
    /// through: report ids are small and repeat across workflows, so a
    /// receiver keyed on the id alone would refuse valid later reports.
    function test_reusedReportIdOnANewAssessmentStillDelivers() public {
        _deliver(_assessment(1), 0x0001);
        _deliver(_assessment(2), 0x0001);

        bytes32 key = oracle.bindingKey(_assessment(2).binding);
        assertEq(oracle.lastSequence(key), 2, "the second report was recorded");
    }

    // --- metadata handling ---

    /// Reading fixed offsets out of a short slice does not revert in
    /// calldata -- it reads whatever follows -- so a truncated delivery
    /// could otherwise produce a workflow owner that happens to match.
    function test_shortMetadataIsRefused() public {
        Assessment memory a = _assessment(1);
        bytes memory truncated =
            abi.encodePacked(WORKFLOW_ID, WORKFLOW_NAME, WORKFLOW_OWNER);
        assertEq(truncated.length, 62, "the packed identity is 62 bytes");

        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.MalformedMetadata.selector, uint256(62))
        );
        forwarder.deliver(address(receiver), truncated, abi.encode(a));
    }

    /// The layout observed on chain, pinned so a decoder change that shifts
    /// an offset fails here rather than on a testnet.
    function test_metadataLayoutMatchesTheForwarder() public pure {
        bytes memory m = _metadata(WORKFLOW_ID, WORKFLOW_OWNER, 0xBEEF);
        assertEq(m.length, 64, "the forwarder delivers 64 bytes");
        assertEq(bytes32(_slice(m, 0, 32)), WORKFLOW_ID, "workflowId at 0");
        assertEq(
            address(bytes20(_slice(m, 42, 20))), WORKFLOW_OWNER,
            "workflowOwner at 42"
        );
        assertEq(bytes2(_slice(m, 62, 2)), bytes2(0xBEEF), "reportId at 62");
    }

    // --- ERC-165 ---

    /// The forwarder checks this before delivering; a receiver that failed
    /// it would simply never be called.
    function test_supportsTheReceiverInterface() public view {
        assertTrue(
            receiver.supportsInterface(IReceiver.onReport.selector),
            "IReceiver"
        );
        assertTrue(receiver.supportsInterface(0x01ffc9a7), "ERC-165");
        assertFalse(receiver.supportsInterface(0xffffffff), "not a wildcard");
    }

    // --- the oracle still applies its own rules ---

    /// The receiver authenticates; it never permits. An expired assessment
    /// is refused by the oracle even when every receiver check passes.
    function test_receiverDoesNotBypassOracleExpiry() public {
        Assessment memory a = _assessment(1);
        a.validUntil = uint64(block.timestamp);

        vm.expectRevert(RiskOracle.Expired.selector);
        _deliver(a, 0x0001);
    }

    /// And a report for another deployment is refused, so a receiver
    /// pointed at the wrong oracle cannot launder an assessment into it.
    function test_receiverDoesNotBypassDeploymentBinding() public {
        Assessment memory a = _assessment(1);
        a.deploymentId = DEPLOYMENT + 1;

        vm.expectRevert(RiskOracle.WrongDeployment.selector);
        _deliver(a, 0x0001);
    }

    /// A receiver that is not registered for the route cannot record
    /// anything, however well-formed its report.
    function test_unregisteredReceiverIsRefused() public {
        CREReceiver rogue = new CREReceiver(
            address(forwarder), WORKFLOW_OWNER, WORKFLOW_ID, oracle);

        Assessment memory a = _assessment(1);
        vm.expectRevert(
            abi.encodeWithSelector(
                RiskOracle.UnauthorizedSigner.selector, address(rogue))
        );
        forwarder.deliver(
            address(rogue),
            _metadata(WORKFLOW_ID, WORKFLOW_OWNER, 0x0001),
            abi.encode(a)
        );
    }

    function _slice(bytes memory b, uint256 start, uint256 len)
        private
        pure
        returns (bytes memory out)
    {
        out = new bytes(len);
        for (uint256 i = 0; i < len; i++) {
            out[i] = b[start + i];
        }
    }
}
