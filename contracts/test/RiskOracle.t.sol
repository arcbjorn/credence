// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {
    Assessment, Binding, Class, Decision, Kind, Permission, Route, Terms
} from "../src/Assessment.sol";

contract RiskOracleTest is Test {
    RiskOracle oracle;

    address constant OPERATOR = address(0xA1);
    address constant ACCOUNT = address(0xB2);
    address constant CONSUMER = address(0xC3);
    uint256 constant DEPLOYMENT = 7;
    uint256 constant FACILITY = 1;

    uint256 devKey = 0xD30;
    uint256 creKey = 0xC12E;
    address devSigner;
    address creSigner;

    function setUp() public {
        devSigner = vm.addr(devKey);
        creSigner = vm.addr(creKey);

        oracle = new RiskOracle(OPERATOR, DEPLOYMENT);
        vm.startPrank(OPERATOR);
        oracle.pinRoute(FACILITY, Route.Development);
        oracle.setSigner(Route.Development, devSigner, true);
        oracle.setSigner(Route.CRE, creSigner, true);
        vm.stopPrank();

        vm.warp(1_000_000);
    }

    function _binding() internal pure returns (Binding memory b) {
        b = Binding({
            identityRegistryChainId: 10143,
            identityRegistry: address(0xAEC0),
            agentId: 184,
            operator: OPERATOR,
            creditAccount: ACCOUNT,
            facilityId: FACILITY,
            accountStateVersion: 3,
            configVersion: 1,
            configHash: keccak256("config"),
            policyEpoch: 1,
            policyHash: keccak256("policy")
        });
    }

    function _origination(uint64 seq, Decision d) internal view returns (Assessment memory a) {
        a = Assessment({
            deploymentId: DEPLOYMENT,
            evidencePolicyId: keccak256("evidence-v1"),
            route: Route.Development,
            binding: _binding(),
            kind: Kind.Origination,
            assessmentId: keccak256(abi.encode("assessment", seq)),
            assessmentHash: keccak256("output"),
            decision: d,
            permission: d == Decision.Approved ? Permission.Allowed : Permission.Blocked,
            modelHash: keccak256("model-v1"),
            inputHash: keccak256("input"),
            evidenceRoot: keccak256("evidence"),
            class_: Class.PrototypeEligible,
            sequence: seq,
            validAfter: uint64(block.timestamp),
            validUntil: uint64(block.timestamp + 1 hours),
            requestId: keccak256("request"),
            intentHash: keccak256("intent"),
            settlementAsset: address(0x05D0),
            requestedPrincipal: 5_000e6,
            terms: d == Decision.Approved
                ? Terms({
                    approvedPrincipal: 5_000e6,
                    aprBps: 1_200,
                    termSeconds: 10 days,
                    requiredEquity: 5_000e6,
                    permittedActionHash: keccak256("action")
                })
                : Terms(0, 0, 0, 0, bytes32(0)),
            loanId: 0
        });
    }

    function _monitoring(uint64 seq, Permission p) internal view returns (Assessment memory a) {
        a = _origination(seq, Decision.Approved);
        a.kind = Kind.Monitoring;
        a.decision = p == Permission.Allowed ? Decision.Approved : Decision.Denied;
        a.permission = p;
        a.requestId = bytes32(0);
        a.requestedPrincipal = 0;
        a.terms = Terms(0, 0, 0, 0, bytes32(0));
        a.loanId = 42;
    }

    function _sign(Assessment memory a, uint256 key) internal view returns (bytes memory) {
        bytes32 digest = _digest(a);
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(key, digest);
        return abi.encodePacked(r, s, v);
    }

    /// Recomputes the EIP-712 digest through the oracle's own view path so the
    /// test cannot drift from the contract's encoding.
    function _digest(Assessment memory a) internal view returns (bytes32) {
        return keccak256(
            abi.encodePacked("\x19\x01", oracle.domainSeparator(), _structHash(a))
        );
    }

    function _structHash(Assessment memory a) internal pure returns (bytes32) {
        bytes32 bindingHash = keccak256(
            abi.encode(
                keccak256(
                    "Binding(uint256 identityRegistryChainId,address identityRegistry,uint256 agentId,address operator,address creditAccount,uint256 facilityId,uint64 accountStateVersion,uint64 configVersion,bytes32 configHash,uint64 policyEpoch,bytes32 policyHash)"
                ),
                a.binding.identityRegistryChainId,
                a.binding.identityRegistry,
                a.binding.agentId,
                a.binding.operator,
                a.binding.creditAccount,
                a.binding.facilityId,
                a.binding.accountStateVersion,
                a.binding.configVersion,
                a.binding.configHash,
                a.binding.policyEpoch,
                a.binding.policyHash
            )
        );
        bytes32 termsHash = keccak256(
            abi.encode(
                keccak256(
                    "Terms(uint256 approvedPrincipal,uint32 aprBps,uint64 termSeconds,uint256 requiredEquity,bytes32 permittedActionHash)"
                ),
                a.terms.approvedPrincipal,
                a.terms.aprBps,
                a.terms.termSeconds,
                a.terms.requiredEquity,
                a.terms.permittedActionHash
            )
        );
        bytes32 head = keccak256(
            abi.encode(
                keccak256(
                    "Assessment(uint256 deploymentId,bytes32 evidencePolicyId,uint8 route,Binding binding,uint8 kind,bytes32 assessmentId,bytes32 assessmentHash,uint8 decision,uint8 permission,bytes32 modelHash,bytes32 inputHash,bytes32 evidenceRoot,uint8 class_,uint64 sequence,uint64 validAfter,uint64 validUntil,bytes32 requestId,bytes32 intentHash,address settlementAsset,uint256 requestedPrincipal,Terms terms,uint256 loanId)Binding(uint256 identityRegistryChainId,address identityRegistry,uint256 agentId,address operator,address creditAccount,uint256 facilityId,uint64 accountStateVersion,uint64 configVersion,bytes32 configHash,uint64 policyEpoch,bytes32 policyHash)Terms(uint256 approvedPrincipal,uint32 aprBps,uint64 termSeconds,uint256 requiredEquity,bytes32 permittedActionHash)"
                ),
                a.deploymentId,
                a.evidencePolicyId,
                uint8(a.route),
                bindingHash,
                uint8(a.kind),
                a.assessmentId,
                a.assessmentHash,
                uint8(a.decision),
                uint8(a.permission)
            )
        );
        bytes32 tail = keccak256(
            abi.encode(
                a.modelHash,
                a.inputHash,
                a.evidenceRoot,
                uint8(a.class_),
                a.sequence,
                a.validAfter,
                a.validUntil,
                a.requestId,
                a.intentHash,
                a.settlementAsset,
                a.requestedPrincipal,
                termsHash,
                a.loanId
            )
        );
        return keccak256(abi.encode(head, tail));
    }

    // --- Happy path ---

    function test_acceptsApprovedOriginationAndStoresTerms() public {
        Assessment memory a = _origination(1, Decision.Approved);
        oracle.accept(a, _sign(a, devKey));

        Binding memory b = _binding();
        (uint256 approved,,,,) = oracle.quoteTerms(oracle.bindingKey(b));
        assertEq(approved, 5_000e6, "terms stored with quote");

        vm.prank(CONSUMER);
        Terms memory t = oracle.consumeQuote(b, 5_000e6, CONSUMER);
        assertEq(t.aprBps, 1_200);
    }

    /// `docs/03`: v0 grants the exact requested amount or denies. A partial
    /// draw cannot consume the quote.
    function test_partialDrawCannotConsumeQuote() public {
        Assessment memory a = _origination(1, Decision.Approved);
        oracle.accept(a, _sign(a, devKey));

        vm.prank(CONSUMER);
        vm.expectRevert(
            abi.encodeWithSelector(RiskOracle.AmountMismatch.selector, 5_000e6, 4_000e6)
        );
        oracle.consumeQuote(_binding(), 4_000e6, CONSUMER);
    }

    function test_quoteConsumedOnlyOnce() public {
        Assessment memory a = _origination(1, Decision.Approved);
        oracle.accept(a, _sign(a, devKey));
        Binding memory b = _binding();

        vm.startPrank(CONSUMER);
        oracle.consumeQuote(b, 5_000e6, CONSUMER);
        vm.expectRevert(RiskOracle.QuoteConsumed.selector);
        oracle.consumeQuote(b, 5_000e6, CONSUMER);
        vm.stopPrank();
    }

    // --- Sequence and supersession ---

    function test_replayedSequenceRejected() public {
        Assessment memory a = _origination(5, Decision.Approved);
        oracle.accept(a, _sign(a, devKey));

        Assessment memory replay = _origination(5, Decision.Approved);
        bytes memory sig = _sign(replay, devKey);
        vm.expectRevert(
            abi.encodeWithSelector(RiskOracle.SequenceNotIncreasing.selector, 5, 5)
        );
        oracle.accept(replay, sig);
    }

    /// `docs/05`: every accepted newer assessment — including a denial —
    /// supersedes an unused older quote.
    function test_denialSupersedesUnusedQuote() public {
        Assessment memory approved = _origination(1, Decision.Approved);
        oracle.accept(approved, _sign(approved, devKey));

        Assessment memory denied = _origination(2, Decision.Denied);
        oracle.accept(denied, _sign(denied, devKey));

        vm.prank(CONSUMER);
        vm.expectRevert(RiskOracle.NoQuote.selector);
        oracle.consumeQuote(_binding(), 5_000e6, CONSUMER);
    }

    /// Monitoring shares the sequence and also supersedes an unused quote.
    function test_monitoringSupersedesUnusedQuote() public {
        Assessment memory approved = _origination(1, Decision.Approved);
        oracle.accept(approved, _sign(approved, devKey));

        Assessment memory mon = _monitoring(2, Permission.Allowed);
        oracle.accept(mon, _sign(mon, devKey));

        vm.prank(CONSUMER);
        vm.expectRevert(RiskOracle.NoQuote.selector);
        oracle.consumeQuote(_binding(), 5_000e6, CONSUMER);
    }

    // --- Route enforcement ---

    /// `docs/05`: a development key is never a substitute in a CRE facility.
    function test_creFacilityRejectsDevelopmentSigner() public {
        vm.prank(OPERATOR);
        oracle.pinRoute(FACILITY, Route.CRE);

        Assessment memory a = _origination(1, Decision.Approved);
        a.route = Route.CRE;
        // Signed by the allowlisted DEVELOPMENT key, not the CRE key.
        bytes memory sig = _sign(a, devKey);
        vm.expectRevert(
            abi.encodeWithSelector(RiskOracle.UnauthorizedSigner.selector, devSigner)
        );
        oracle.accept(a, sig);
    }

    function test_presentedRouteMustMatchPinned() public {
        Assessment memory a = _origination(1, Decision.Approved);
        a.route = Route.CRE; // facility is pinned to Development
        bytes memory sig = _sign(a, creKey);
        vm.expectRevert(
            abi.encodeWithSelector(RiskOracle.WrongRoute.selector, Route.Development, Route.CRE)
        );
        oracle.accept(a, sig);
    }

    function test_unpinnedFacilityRejected() public {
        Assessment memory a = _origination(1, Decision.Approved);
        a.binding.facilityId = 99;
        bytes memory sig = _sign(a, devKey);
        vm.expectRevert(abi.encodeWithSelector(RiskOracle.RouteNotPinned.selector, 99));
        oracle.accept(a, sig);
    }

    // --- Field shape ---

    /// `docs/05`: reject grant fields on a non-approved result.
    function test_grantOnDenialRejected() public {
        Assessment memory a = _origination(1, Decision.Denied);
        a.terms.approvedPrincipal = 5_000e6; // malformed
        bytes memory sig = _sign(a, devKey);
        vm.expectRevert(RiskOracle.GrantOnNonApproval.selector);
        oracle.accept(a, sig);
    }

    /// Monitoring can never disburse, so terms on it are malformed.
    function test_grantOnMonitoringRejected() public {
        Assessment memory a = _monitoring(1, Permission.Allowed);
        a.terms.approvedPrincipal = 1;
        bytes memory sig = _sign(a, devKey);
        vm.expectRevert(RiskOracle.GrantOnMonitoring.selector);
        oracle.accept(a, sig);
    }

    // --- Class and validity ---

    /// `docs/04`: an observed/shadow approval is non-executable by class.
    function test_shadowApprovalStoresNoQuote() public {
        Assessment memory a = _origination(1, Decision.Approved);
        a.class_ = Class.ObservedShadow;
        oracle.accept(a, _sign(a, devKey));

        vm.prank(CONSUMER);
        vm.expectRevert(RiskOracle.NoQuote.selector);
        oracle.consumeQuote(_binding(), 5_000e6, CONSUMER);
    }

    function test_expiredAssessmentRejected() public {
        Assessment memory a = _origination(1, Decision.Approved);
        bytes memory sig = _sign(a, devKey);
        vm.warp(block.timestamp + 2 hours);
        vm.expectRevert(RiskOracle.Expired.selector);
        oracle.accept(a, sig);
    }

    function test_wrongDeploymentRejected() public {
        Assessment memory a = _origination(1, Decision.Approved);
        a.deploymentId = DEPLOYMENT + 1;
        bytes memory sig = _sign(a, devKey);
        vm.expectRevert(RiskOracle.WrongDeployment.selector);
        oracle.accept(a, sig);
    }

    function test_unauthorizedSignerRejected() public {
        Assessment memory a = _origination(1, Decision.Approved);
        bytes memory sig = _sign(a, 0xBAD);
        vm.expectRevert(
            abi.encodeWithSelector(RiskOracle.UnauthorizedSigner.selector, vm.addr(0xBAD))
        );
        oracle.accept(a, sig);
    }

    /// Any tampered field breaks the signature, because every economically
    /// material field is inside the digest.
    function test_tamperedFieldBreaksSignature() public {
        Assessment memory a = _origination(1, Decision.Approved);
        bytes memory sig = _sign(a, devKey);
        a.terms.approvedPrincipal = 50_000e6; // inflate after signing
        vm.expectRevert();
        oracle.accept(a, sig);
    }

    // --- Monitoring permission ---

    function test_riskPermissionRequiresMatchingStateVersion() public {
        Assessment memory mon = _monitoring(1, Permission.Allowed);
        oracle.accept(mon, _sign(mon, devKey));

        Binding memory b = _binding();
        assertTrue(oracle.riskAllowed(b), "allowed at issued state version");

        // The account executed since: prior permission is void.
        b.accountStateVersion = 4;
        assertFalse(oracle.riskAllowed(b), "stale state version blocks risk");
    }

    function test_blockedPermissionDeniesRisk() public {
        Assessment memory mon = _monitoring(1, Permission.Blocked);
        oracle.accept(mon, _sign(mon, devKey));
        assertFalse(oracle.riskAllowed(_binding()));
    }

    function test_permissionExpires() public {
        Assessment memory mon = _monitoring(1, Permission.Allowed);
        oracle.accept(mon, _sign(mon, devKey));
        assertTrue(oracle.riskAllowed(_binding()));

        vm.warp(block.timestamp + 2 hours);
        assertFalse(oracle.riskAllowed(_binding()), "expired permission blocks risk");
    }
}
