// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Vm} from "forge-std/Vm.sol";
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

/// @notice Builds and signs assessments for tests.
/// @dev Shared so the EIP-712 encoding lives in exactly one place. If the
/// oracle's struct hashing drifts from this, every suite fails at once
/// rather than one suite silently testing a stale encoding.
library OracleHarness {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    bytes32 private constant BINDING_TYPEHASH = keccak256(
        "Binding(uint256 identityRegistryChainId,address identityRegistry,uint256 agentId,address operator,address creditAccount,uint256 facilityId,uint64 accountStateVersion,uint64 configVersion,bytes32 configHash,uint64 policyEpoch,bytes32 policyHash)"
    );
    bytes32 private constant TERMS_TYPEHASH = keccak256(
        "Terms(uint256 approvedPrincipal,uint32 aprBps,uint64 termSeconds,uint256 requiredEquity,bytes32 permittedActionHash)"
    );
    bytes32 private constant ASSESSMENT_TYPEHASH = keccak256(
        "Assessment(uint256 deploymentId,bytes32 evidencePolicyId,uint8 route,Binding binding,uint8 kind,bytes32 assessmentId,bytes32 assessmentHash,uint8 decision,uint8 permission,bytes32 modelHash,bytes32 inputHash,bytes32 evidenceRoot,uint8 class_,uint64 sequence,uint64 validAfter,uint64 validUntil,bytes32 requestId,bytes32 intentHash,address settlementAsset,uint256 requestedPrincipal,Terms terms,uint256 loanId)Binding(uint256 identityRegistryChainId,address identityRegistry,uint256 agentId,address operator,address creditAccount,uint256 facilityId,uint64 accountStateVersion,uint64 configVersion,bytes32 configHash,uint64 policyEpoch,bytes32 policyHash)Terms(uint256 approvedPrincipal,uint32 aprBps,uint64 termSeconds,uint256 requiredEquity,bytes32 permittedActionHash)"
    );

    function binding(address account, uint64 stateVersion, uint256 facilityId)
        internal
        pure
        returns (Binding memory b)
    {
        b = Binding({
            identityRegistryChainId: 10143,
            identityRegistry: address(0xAEC0),
            agentId: 184,
            operator: address(0xA1),
            creditAccount: account,
            facilityId: facilityId,
            accountStateVersion: stateVersion,
            configVersion: 1,
            configHash: keccak256("config"),
            policyEpoch: 1,
            policyHash: keccak256("policy")
        });
    }

    /// A monitoring result that grants standing permission.
    function monitoringAllowed(
        Binding memory b,
        uint64 sequence,
        uint256 deploymentId,
        uint64 nowTs
    ) internal pure returns (Assessment memory a) {
        a = monitoringBlocked(b, sequence, deploymentId, nowTs);
        a.decision = Decision.Approved;
        a.permission = Permission.Allowed;
    }

    /// A monitoring result that withdraws permission.
    ///
    /// Distinct from letting one lapse: this is the attestor SAYING the
    /// loan may no longer take risk, which `docs/05` treats as a recovery
    /// trigger, where silence is not.
    function monitoringBlocked(
        Binding memory b,
        uint64 sequence,
        uint256 deploymentId,
        uint64 nowTs
    ) internal pure returns (Assessment memory a) {
        a = Assessment({
            deploymentId: deploymentId,
            evidencePolicyId: keccak256("evidence-v1"),
            route: Route.Development,
            binding: b,
            kind: Kind.Monitoring,
            assessmentId: keccak256(abi.encode("monitoring", sequence, b.creditAccount)),
            assessmentHash: keccak256("output"),
            decision: Decision.Denied,
            permission: Permission.Blocked,
            modelHash: keccak256("model-v1"),
            inputHash: keccak256("input"),
            evidenceRoot: keccak256("evidence"),
            class_: Class.PrototypeEligible,
            sequence: sequence,
            validAfter: nowTs,
            validUntil: nowTs + 1 hours,
            // Monitoring carries no grant: `docs/05` forbids it and the
            // oracle rejects a result that does.
            requestId: bytes32(0),
            intentHash: bytes32(0),
            settlementAsset: address(0),
            requestedPrincipal: 0,
            terms: Terms({
                approvedPrincipal: 0,
                aprBps: 0,
                termSeconds: 0,
                requiredEquity: 0,
                permittedActionHash: bytes32(0)
            }),
            loanId: 1
        });
    }

    function approvedQuote(
        Binding memory b,
        uint64 sequence,
        uint256 principal,
        uint32 aprBps,
        uint64 termSeconds,
        uint256 requiredEquity,
        address settlementAsset,
        uint256 deploymentId,
        uint64 nowTs
    ) internal pure returns (Assessment memory a) {
        return approvedQuote(
            b,
            sequence,
            principal,
            aprBps,
            termSeconds,
            requiredEquity,
            settlementAsset,
            deploymentId,
            nowTs,
            bytes32(0)
        );
    }

    /// The same quote, committing to one exact action.
    ///
    /// `docs/05`: a quote authorizes THAT call, not any call through the
    /// same adapter. A zero hash means the approved action is to hold
    /// settlement tokens.
    function approvedQuote(
        Binding memory b,
        uint64 sequence,
        uint256 principal,
        uint32 aprBps,
        uint64 termSeconds,
        uint256 requiredEquity,
        address settlementAsset,
        uint256 deploymentId,
        uint64 nowTs,
        bytes32 actionHash
    ) internal pure returns (Assessment memory a) {
        a = Assessment({
            deploymentId: deploymentId,
            evidencePolicyId: keccak256("evidence-v1"),
            route: Route.Development,
            binding: b,
            kind: Kind.Origination,
            assessmentId: keccak256(abi.encode("assessment", sequence, b.creditAccount)),
            assessmentHash: keccak256("output"),
            decision: Decision.Approved,
            permission: Permission.Allowed,
            modelHash: keccak256("model-v1"),
            inputHash: keccak256("input"),
            evidenceRoot: keccak256("evidence"),
            class_: Class.PrototypeEligible,
            sequence: sequence,
            validAfter: nowTs,
            validUntil: nowTs + 1 hours,
            requestId: keccak256(abi.encode("request", sequence)),
            intentHash: keccak256("intent"),
            settlementAsset: settlementAsset,
            requestedPrincipal: principal,
            terms: Terms({
                approvedPrincipal: principal,
                aprBps: aprBps,
                termSeconds: termSeconds,
                requiredEquity: requiredEquity,
                permittedActionHash: actionHash
            }),
            loanId: 0
        });
    }

    /// A signed monitoring result, accepted onto the oracle.
    ///
    /// Exists so a test can establish standing permission without
    /// assembling the whole envelope, which is what makes "recovery needs
    /// a reason" testable at all.
    function publishMonitoring(
        RiskOracle oracle,
        Binding memory b,
        uint64 sequence,
        uint256 key,
        bool allowed,
        uint256 deploymentId,
        uint64 nowTs
    ) internal {
        Assessment memory a = Assessment({
            deploymentId: deploymentId,
            evidencePolicyId: keccak256("evidence-v1"),
            route: Route.Development,
            binding: b,
            kind: Kind.Monitoring,
            assessmentId: keccak256(abi.encode("monitoring", sequence, b.creditAccount)),
            assessmentHash: keccak256("output"),
            decision: allowed ? Decision.Approved : Decision.Denied,
            permission: allowed ? Permission.Allowed : Permission.Blocked,
            modelHash: keccak256("model-v1"),
            inputHash: keccak256("input"),
            evidenceRoot: keccak256("evidence"),
            class_: Class.PrototypeEligible,
            sequence: sequence,
            validAfter: nowTs,
            validUntil: nowTs + 1 hours,
            requestId: bytes32(0),
            intentHash: keccak256("intent"),
            settlementAsset: address(0),
            requestedPrincipal: 0,
            terms: Terms(0, 0, 0, 0, bytes32(0)),
            loanId: 1
        });
        oracle.accept(a, sign(oracle, a, key));
    }

    function sign(RiskOracle oracle, Assessment memory a, uint256 key)
        internal
        view
        returns (bytes memory)
    {
        bytes32 digest =
            keccak256(abi.encodePacked("\x19\x01", oracle.domainSeparator(), structHash(a)));
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(key, digest);
        return abi.encodePacked(r, s, v);
    }

    function structHash(Assessment memory a) internal pure returns (bytes32) {
        bytes32 head = keccak256(
            abi.encode(
                ASSESSMENT_TYPEHASH,
                a.deploymentId,
                a.evidencePolicyId,
                uint8(a.route),
                bindingHash(a.binding),
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
                termsHash(a.terms),
                a.loanId
            )
        );
        return keccak256(abi.encode(head, tail));
    }

    function bindingHash(Binding memory b) internal pure returns (bytes32) {
        return keccak256(
            abi.encode(
                BINDING_TYPEHASH,
                b.identityRegistryChainId,
                b.identityRegistry,
                b.agentId,
                b.operator,
                b.creditAccount,
                b.facilityId,
                b.accountStateVersion,
                b.configVersion,
                b.configHash,
                b.policyEpoch,
                b.policyHash
            )
        );
    }

    function termsHash(Terms memory t) internal pure returns (bytes32) {
        return keccak256(
            abi.encode(
                TERMS_TYPEHASH,
                t.approvedPrincipal,
                t.aprBps,
                t.termSeconds,
                t.requiredEquity,
                t.permittedActionHash
            )
        );
    }
}
