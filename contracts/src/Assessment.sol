// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @notice Which route may publish executable assessments for a facility.
/// @dev Pinned per epoch. A development key is never a substitute in a CRE
/// facility (`docs/05`), so this is not a preference — it is a hard gate.
enum Route {
    Unset,
    Development,
    CRE
}

enum Kind {
    Origination,
    Monitoring
}

enum Decision {
    Approved,
    Denied,
    Unavailable
}

/// @dev Separate from `Decision` on purpose. Permission and grant are
/// different things: a monitoring result carries permission but never a
/// grant, and a denied origination carries neither.
enum Permission {
    Blocked,
    Allowed
}

/// @notice Evidence class. Only `PrototypeEligible` may move capital.
/// @dev `docs/04`: an observed/shadow approval is non-executable by its
/// authorization class, regardless of how it was signed.
enum Class {
    ScenarioOnly,
    ObservedShadow,
    PrototypeEligible
}

/// @notice The binding an assessment is scoped to.
/// @dev Reference agents by registry chain and address, never a bare integer
/// (`docs/06`). Policy and config each carry an identifier AND a hash;
/// neither pair is an alias for the other.
struct Binding {
    uint256 identityRegistryChainId;
    address identityRegistry;
    uint256 agentId;
    address operator;
    address creditAccount;
    uint256 facilityId;
    uint64 accountStateVersion;
    uint64 configVersion;
    bytes32 configHash;
    uint64 policyEpoch;
    bytes32 policyHash;
}

/// @notice Terms attached to an approved origination. Absent otherwise.
struct Terms {
    uint256 approvedPrincipal;
    uint32 aprBps;
    uint64 termSeconds;
    uint256 requiredEquity;
    bytes32 permittedActionHash;
}

/// @notice One assessment, either kind.
/// @dev Kind-specific fields are present or zero per `docs/05`. The oracle
/// rejects grant fields on non-approved and on every monitoring result, so
/// a zeroed field is enforced rather than merely conventional.
struct Assessment {
    uint256 deploymentId;
    bytes32 evidencePolicyId;
    Route route;
    Binding binding;
    Kind kind;
    bytes32 assessmentId;
    bytes32 assessmentHash;
    Decision decision;
    Permission permission;
    bytes32 modelHash;
    bytes32 inputHash;
    bytes32 evidenceRoot;
    Class class_;
    uint64 sequence;
    uint64 validAfter;
    uint64 validUntil;
    // Origination only
    bytes32 requestId;
    bytes32 intentHash;
    address settlementAsset;
    uint256 requestedPrincipal;
    Terms terms;
    // Monitoring only
    uint256 loanId;
}
