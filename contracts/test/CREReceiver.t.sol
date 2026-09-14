// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {
    CREReceiver, IReceiver, MonitoringReport
} from "../src/CREReceiver.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {
    Binding, Route, Decision, Permission, Class
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
    uint256 constant LOAN = 1;
    bytes32 constant EVIDENCE_POLICY = keccak256("evidence-v1");

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
            address(forwarder), WORKFLOW_OWNER, WORKFLOW_ID, oracle,
            OPERATOR, DEPLOYMENT, EVIDENCE_POLICY
        );

        vm.startPrank(OPERATOR);
        oracle.setLoanManager(CONSUMER);
        oracle.pinRoute(FACILITY, Route.CRE);
        // The receiver IS the CRE route's authority: a report carries no
        // ECDSA signature, so the caller is what the oracle trusts.
        oracle.setSigner(Route.CRE, address(receiver), true);
        receiver.enrol(LOAN, _binding());
        vm.stopPrank();

        vm.warp(1_000_000);
    }

    // --- helpers ---

    function _binding() internal pure returns (Binding memory b) {
        b = Binding({
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
        });
    }

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

    function _report(uint64 seq) internal view returns (MonitoringReport memory) {
        return MonitoringReport({
            loanId: LOAN,
            accountStateVersion: 0,
            sequence: seq,
            validAfter: uint64(block.timestamp),
            validUntil: uint64(block.timestamp + 1 hours),
            decision: Decision.Approved,
            permission: Permission.Allowed,
            class_: Class.PrototypeEligible,
            assessmentId: keccak256(abi.encode("assessment", seq)),
            assessmentHash: keccak256("output"),
            modelHash: keccak256("model-v1"),
            inputHash: keccak256("input"),
            evidenceRoot: keccak256("evidence")
        });
    }

    function _deliver(MonitoringReport memory r, bytes2 reportId) internal {
        forwarder.deliver(
            address(receiver),
            _metadata(WORKFLOW_ID, WORKFLOW_OWNER, reportId),
            abi.encode(r)
        );
    }

    // --- the happy path ---

    /// A well-formed report from the expected forwarder and workflow
    /// reaches the oracle and updates permission, with no signature
    /// anywhere in the path.
    function test_validReportUpdatesPermission() public {
        _deliver(_report(1), 0x0001);

        Binding memory b = _binding();
        bytes32 key = oracle.bindingKey(b);
        assertEq(oracle.lastSequence(key), 1, "the oracle recorded it");
        assertTrue(
            oracle.riskAllowedFor(
                key, oracle.bindingConfigHash(b), b.accountStateVersion),
            "permission stands"
        );
    }

    /// A Blocked report withdraws permission rather than failing: that is
    /// the whole point of continuous underwriting.
    function test_blockedReportWithdrawsPermission() public {
        _deliver(_report(1), 0x0001);

        MonitoringReport memory r = _report(2);
        r.decision = Decision.Denied;
        r.permission = Permission.Blocked;
        _deliver(r, 0x0002);

        Binding memory b = _binding();
        bytes32 key = oracle.bindingKey(b);
        assertFalse(
            oracle.riskAllowedFor(
                key, oracle.bindingConfigHash(b), b.accountStateVersion),
            "permission was withdrawn"
        );
    }

    // --- check 1: the forwarder ---

    /// Without this the DON is decorative: anyone calls onReport directly
    /// and authors their own assessment.
    function test_onlyTheForwarderMayDeliver() public {
        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.UnexpectedSender.selector,
                address(this), address(forwarder))
        );
        receiver.onReport(
            _metadata(WORKFLOW_ID, WORKFLOW_OWNER, 0x0001),
            abi.encode(_report(1))
        );
    }

    // --- check 2: the workflow owner ---

    /// One forwarder routes for every workflow on the chain, so it attests
    /// that SOME workflow produced this, never whose.
    function test_foreignWorkflowOwnerIsRefused() public {
        address intruder = address(0xBAD);
        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.UnexpectedWorkflowOwner.selector,
                intruder, WORKFLOW_OWNER)
        );
        forwarder.deliver(
            address(receiver),
            _metadata(WORKFLOW_ID, intruder, 0x0001),
            abi.encode(_report(1))
        );
    }

    // --- check 3: the workflow id ---

    /// An owner may run several workflows; only one underwrites. A
    /// different workflow of the same owner is not an underwriting result.
    function test_foreignWorkflowIdIsRefused() public {
        bytes32 other = bytes32(uint256(0xDEAD));
        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.UnexpectedWorkflow.selector, other, WORKFLOW_ID)
        );
        forwarder.deliver(
            address(receiver),
            _metadata(other, WORKFLOW_OWNER, 0x0001),
            abi.encode(_report(1))
        );
    }

    // --- check 4: enrolment ---

    /// The workflow names a loan; the OPERATOR says what that loan is. A
    /// workflow that could supply its own binding could move an obligation
    /// to an account nobody assessed.
    function test_unenrolledLoanIsRefused() public {
        MonitoringReport memory r = _report(1);
        r.loanId = 99;
        vm.expectRevert(
            abi.encodeWithSelector(CREReceiver.LoanNotEnrolled.selector, uint256(99))
        );
        _deliver(r, 0x0001);
    }

    /// Retiring a resolved loan stops further reports. Revocation is always
    /// available because withdrawing a route only reduces what can be
    /// authorized (`docs/05` invariant 11).
    function test_retiredLoanIsRefused() public {
        vm.prank(OPERATOR);
        receiver.retire(LOAN);

        vm.expectRevert(
            abi.encodeWithSelector(CREReceiver.LoanNotEnrolled.selector, LOAN)
        );
        _deliver(_report(1), 0x0001);
    }

    function test_onlyOperatorEnrols() public {
        vm.expectRevert(CREReceiver.NotOperator.selector);
        receiver.enrol(2, _binding());
    }

    // --- check 5: replay ---

    /// The forwarder retries a reverted delivery with more gas, so a
    /// receiver that acted twice would let one DON round advance the
    /// sequence twice (`docs/05` invariant 2).
    function test_sameReportCannotBeDeliveredTwice() public {
        _deliver(_report(1), 0x0001);
        vm.expectRevert();
        _deliver(_report(1), 0x0001);
    }

    /// But a genuinely new report with a reused two-byte id must still get
    /// through: report ids are small and repeat across workflows, so a
    /// receiver keyed on the id alone would refuse valid later reports.
    function test_reusedReportIdOnANewAssessmentStillDelivers() public {
        _deliver(_report(1), 0x0001);
        _deliver(_report(2), 0x0001);

        assertEq(
            oracle.lastSequence(oracle.bindingKey(_binding())), 2,
            "the second report was recorded"
        );
    }

    // --- the payload cannot carry a grant ---

    /// `docs/05`: monitoring can never disburse. The report type has no
    /// field for terms, a requested principal, or a request id, so this is
    /// enforced by the shape of the message rather than by a check.
    function test_monitoringPayloadHasNoGrantFields() public {
        _deliver(_report(1), 0x0001);

        // What reached the oracle carries no grant: if it did,
        // _checkFieldShape would have reverted with GrantOnMonitoring.
        assertEq(
            oracle.lastSequence(oracle.bindingKey(_binding())), 1,
            "accepted, so the field shape was clean"
        );
    }

    /// The version the ENGINE assessed rides in the report. Permission
    /// against a version the account has since moved past must not stand.
    function test_permissionIsScopedToTheAssessedStateVersion() public {
        MonitoringReport memory r = _report(1);
        r.accountStateVersion = 5;
        _deliver(r, 0x0001);

        Binding memory b = _binding();
        bytes32 key = oracle.bindingKey(b);
        assertTrue(
            oracle.riskAllowedFor(key, oracle.bindingConfigHash(b), 5),
            "permission stands for the version assessed"
        );
        assertFalse(
            oracle.riskAllowedFor(key, oracle.bindingConfigHash(b), 6),
            "and not for a later one"
        );
    }

    /// The workflow encodes this payload in Go, so the layout has to be
    /// something a hand-written encoder can produce exactly. Every field
    /// is static, so it is 13 packed words with no offset word and no
    /// dynamic tail -- which is why the Go side can build it without an
    /// ABI library.
    function test_payloadIsThirteenStaticWords() public view {
        bytes memory enc = abi.encode(_report(1));
        assertEq(enc.length, 13 * 32, "13 static words, no offset");

        MonitoringReport memory back = abi.decode(enc, (MonitoringReport));
        assertEq(back.loanId, LOAN, "round trips");
        assertEq(back.sequence, 1);
        assertEq(uint8(back.class_), uint8(Class.PrototypeEligible));
    }

    // --- metadata handling ---

    /// Reading fixed offsets out of a short slice does not revert in
    /// calldata -- it reads whatever follows -- so a truncated delivery
    /// could otherwise produce a workflow owner that happens to match.
    function test_shortMetadataIsRefused() public {
        bytes memory truncated =
            abi.encodePacked(WORKFLOW_ID, WORKFLOW_NAME, WORKFLOW_OWNER);
        assertEq(truncated.length, 62, "the packed identity is 62 bytes");

        vm.expectRevert(
            abi.encodeWithSelector(
                CREReceiver.MalformedMetadata.selector, uint256(62))
        );
        forwarder.deliver(
            address(receiver), truncated, abi.encode(_report(1)));
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
            receiver.supportsInterface(IReceiver.onReport.selector), "IReceiver");
        assertTrue(receiver.supportsInterface(0x01ffc9a7), "ERC-165");
        assertFalse(receiver.supportsInterface(0xffffffff), "not a wildcard");
    }

    // --- the oracle still applies its own rules ---

    /// The receiver authenticates; it never permits. An expired assessment
    /// is refused by the oracle even when every receiver check passes.
    function test_receiverDoesNotBypassOracleExpiry() public {
        MonitoringReport memory r = _report(1);
        r.validUntil = uint64(block.timestamp);

        vm.expectRevert(RiskOracle.Expired.selector);
        _deliver(r, 0x0001);
    }

    /// A sequence that does not increase is a replay at the oracle level,
    /// independent of the receiver's own report-id guard.
    function test_receiverDoesNotBypassSequenceOrdering() public {
        _deliver(_report(2), 0x0001);

        vm.expectRevert(
            abi.encodeWithSelector(
                RiskOracle.SequenceNotIncreasing.selector, uint64(2), uint64(1))
        );
        _deliver(_report(1), 0x0002);
    }

    /// A receiver that is not registered for the route cannot record
    /// anything, however well-formed its report.
    function test_unregisteredReceiverIsRefused() public {
        CREReceiver rogue = new CREReceiver(
            address(forwarder), WORKFLOW_OWNER, WORKFLOW_ID, oracle,
            OPERATOR, DEPLOYMENT, EVIDENCE_POLICY
        );
        vm.prank(OPERATOR);
        rogue.enrol(LOAN, _binding());

        vm.expectRevert(
            abi.encodeWithSelector(
                RiskOracle.UnauthorizedSigner.selector, address(rogue))
        );
        forwarder.deliver(
            address(rogue),
            _metadata(WORKFLOW_ID, WORKFLOW_OWNER, 0x0001),
            abi.encode(_report(1))
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
