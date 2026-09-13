// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Assessment, Binding, Class, Decision, Kind, Permission, Route, Terms} from "./Assessment.sol";

/// @title RiskOracle
/// @notice Accepts signed assessments and holds the one quote or permission
/// currently in force for a binding.
///
/// This contract does not compute risk. It enforces that a result arrived
/// through the facility's pinned route, is correctly scoped, has not been
/// replayed or superseded, and carries only the fields its kind permits.
///
/// Two rules carry most of the weight (`docs/05`):
///   - A **single strictly increasing sequence per binding across both
///     kinds**. Any accepted newer assessment — including Denied,
///     Unavailable, or monitoring — supersedes an unused older quote.
///   - Current contract state is still checked at draw time. A correctly
///     signed assessment is not permission to ignore a vault cap or an
///     open loan.
contract RiskOracle {
    bytes32 private constant BINDING_TYPEHASH = keccak256(
        "Binding(uint256 identityRegistryChainId,address identityRegistry,uint256 agentId,address operator,address creditAccount,uint256 facilityId,uint64 accountStateVersion,uint64 configVersion,bytes32 configHash,uint64 policyEpoch,bytes32 policyHash)"
    );
    bytes32 private constant TERMS_TYPEHASH = keccak256(
        "Terms(uint256 approvedPrincipal,uint32 aprBps,uint64 termSeconds,uint256 requiredEquity,bytes32 permittedActionHash)"
    );
    bytes32 private constant ASSESSMENT_TYPEHASH = keccak256(
        "Assessment(uint256 deploymentId,bytes32 evidencePolicyId,uint8 route,Binding binding,uint8 kind,bytes32 assessmentId,bytes32 assessmentHash,uint8 decision,uint8 permission,bytes32 modelHash,bytes32 inputHash,bytes32 evidenceRoot,uint8 class_,uint64 sequence,uint64 validAfter,uint64 validUntil,bytes32 requestId,bytes32 intentHash,address settlementAsset,uint256 requestedPrincipal,Terms terms,uint256 loanId)Binding(uint256 identityRegistryChainId,address identityRegistry,uint256 agentId,address operator,address creditAccount,uint256 facilityId,uint64 accountStateVersion,uint64 configVersion,bytes32 configHash,uint64 policyEpoch,bytes32 policyHash)Terms(uint256 approvedPrincipal,uint32 aprBps,uint64 termSeconds,uint256 requiredEquity,bytes32 permittedActionHash)"
    );
    bytes32 private constant DOMAIN_TYPEHASH = keccak256(
        "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"
    );

    /// @dev A consumed quote records which assessment consumed it, so a
    /// replay attempt is distinguishable from a never-issued one.
    struct Stored {
        bytes32 assessmentId;
        uint64 sequence;
        bool consumed;
        // The window the quote may fund in. Retained because accept() runs
        // once while consumption happens later: without it the evidence
        // behind an accepted quote ages out and nothing on chain notices.
        uint64 validUntil;
    }

    address public immutable operator;
    uint256 public immutable deploymentId;
    bytes32 public immutable domainSeparator;

    /// @dev The only contract permitted to consume quotes.
    address public loanManager;

    /// @dev Facility route, pinned per epoch.
    mapping(uint256 facilityId => Route) public facilityRoute;
    /// @dev Signer allowlists are scoped WITHIN a route, so a development key
    /// cannot authorize in a CRE facility even if separately allowlisted.
    mapping(Route => mapping(address signer => bool)) public routeSigner;

    /// @dev Highest sequence accepted for a binding, across both kinds.
    mapping(bytes32 bindingKey => uint64) public lastSequence;
    /// @dev The origination quote currently in force, if any.
    mapping(bytes32 bindingKey => Stored) public quote;
    /// @dev Terms attached to that quote. Written with it, cleared with it.
    mapping(bytes32 bindingKey => Terms) public quoteTerms;
    /// @dev Monitoring permission currently in force.
    mapping(bytes32 bindingKey => Permission) public riskPermission;
    /// @dev Account state version the permission was issued against.
    mapping(bytes32 bindingKey => uint64) public permissionStateVersion;
    mapping(bytes32 bindingKey => uint64) public permissionValidUntil;

    error NotOperator();
    error RouteNotPinned(uint256 facilityId);
    error WrongRoute(Route pinned, Route presented);
    error UnauthorizedSigner(address signer);
    error WrongDeployment();
    error SequenceNotIncreasing(uint64 last, uint64 presented);
    error NotYetValid();
    error Expired();
    error GrantOnNonApproval();
    error GrantOnMonitoring();
    error NotExecutableClass(Class presented);
    error NoQuote();
    error QuoteConsumed();
    error QuoteExpired();
    error GrantMismatch(uint256 requested, uint256 approved);
    error AmountMismatch(uint256 approved, uint256 requested);
    error WrongKind();
    error NotConsumer();

    event RoutePinned(uint256 indexed facilityId, Route route);
    event SignerSet(Route indexed route, address indexed signer, bool allowed);
    event Accepted(bytes32 indexed bindingKey, bytes32 indexed assessmentId, Kind kind, uint64 sequence);
    event QuoteConsumedEvent(bytes32 indexed bindingKey, bytes32 indexed assessmentId);

    modifier onlyOperator() {
        require(msg.sender == operator, NotOperator());
        _;
    }

    constructor(address operator_, uint256 deploymentId_) {
        operator = operator_;
        deploymentId = deploymentId_;
        domainSeparator = keccak256(
            abi.encode(
                DOMAIN_TYPEHASH,
                keccak256("Credence"),
                keccak256("1"),
                block.chainid,
                address(this)
            )
        );
    }

    function setLoanManager(address loanManager_) external onlyOperator {
        loanManager = loanManager_;
    }

    function pinRoute(uint256 facilityId, Route route) external onlyOperator {
        facilityRoute[facilityId] = route;
        emit RoutePinned(facilityId, route);
    }

    function setSigner(Route route, address signer, bool allowed) external onlyOperator {
        routeSigner[route][signer] = allowed;
        emit SignerSet(route, signer, allowed);
    }

    function bindingKey(Binding calldata b) public pure returns (bytes32) {
        return keccak256(
            abi.encode(
                b.identityRegistryChainId,
                b.identityRegistry,
                b.agentId,
                b.creditAccount,
                b.facilityId
            )
        );
    }

    // --- Acceptance ---

    /// @notice Verify and record an assessment.
    /// @dev Rejects replayed, expired, cross-deployment, wrong-route, and
    /// mis-scoped attestations. Field-shape rules are enforced, not assumed:
    /// a grant on a denial or on any monitoring result is rejected outright.
    function accept(Assessment calldata a, bytes calldata signature) external {
        require(a.deploymentId == deploymentId, WrongDeployment());

        Route pinned = facilityRoute[a.binding.facilityId];
        require(pinned != Route.Unset, RouteNotPinned(a.binding.facilityId));
        require(a.route == pinned, WrongRoute(pinned, a.route));

        address signer = _recover(_hash(a), signature);
        require(routeSigner[pinned][signer], UnauthorizedSigner(signer));

        require(block.timestamp >= a.validAfter, NotYetValid());
        require(block.timestamp < a.validUntil, Expired());

        bytes32 key = bindingKey(a.binding);
        uint64 last = lastSequence[key];
        require(a.sequence > last, SequenceNotIncreasing(last, a.sequence));

        _checkFieldShape(a);

        lastSequence[key] = a.sequence;

        // Any accepted newer assessment supersedes an unused older quote —
        // including a denial, an unavailable, or a monitoring update.
        delete quote[key];
        delete quoteTerms[key];

        if (a.kind == Kind.Origination) {
            if (a.decision == Decision.Approved && a.class_ == Class.PrototypeEligible) {
                quote[key] = Stored({
                    assessmentId: a.assessmentId,
                    sequence: a.sequence,
                    consumed: false,
                    validUntil: a.validUntil
                });
                quoteTerms[key] = a.terms;
            }
        } else {
            riskPermission[key] = a.permission;
            permissionStateVersion[key] = a.binding.accountStateVersion;
            permissionValidUntil[key] = a.validUntil;
        }

        emit Accepted(key, a.assessmentId, a.kind, a.sequence);
    }

    /// @dev `docs/05`: reject grant fields on a non-approved result, and on
    /// every monitoring result. Monitoring can never disburse, so carrying
    /// terms at all is a malformed message rather than a harmless extra.
    function _checkFieldShape(Assessment calldata a) private pure {
        bool hasGrant = a.terms.approvedPrincipal != 0 || a.terms.aprBps != 0
            || a.terms.termSeconds != 0 || a.terms.requiredEquity != 0
            || a.terms.permittedActionHash != bytes32(0);

        if (a.kind == Kind.Monitoring) {
            require(!hasGrant, GrantOnMonitoring());
            require(a.requestedPrincipal == 0, GrantOnMonitoring());
            require(a.requestId == bytes32(0), GrantOnMonitoring());
        } else if (a.decision != Decision.Approved) {
            require(!hasGrant, GrantOnNonApproval());
        } else {
            // `docs/03`: v0 grants the exact requested amount or denies.
            // An approval for more than was asked is malformed, and
            // trusting the attestor's arithmetic here would let a signed
            // output fund beyond what the agent ever requested.
            require(
                a.terms.approvedPrincipal == a.requestedPrincipal,
                GrantMismatch(a.requestedPrincipal, a.terms.approvedPrincipal)
            );
        }
    }

    // --- Consumption ---

    /// @notice Consume the standing quote for an exact draw.
    /// @dev `docs/03`: v0 grants the exact requested amount or denies. A
    /// smaller or larger draw cannot consume the quote.
    ///
    /// Restricted to the registered loan manager. Anyone else could
    /// otherwise burn a live quote by calling this directly — griefing that
    /// costs nothing and forces a fresh assessment.
    function consumeQuote(Binding calldata b, uint256 drawAmount)
        external
        returns (Terms memory)
    {
        require(msg.sender == loanManager, NotConsumer());
        bytes32 key = bindingKey(b);
        Stored storage q = quote[key];
        require(q.assessmentId != bytes32(0), NoQuote());
        require(!q.consumed, QuoteConsumed());
        // Validity binds here, not only at accept: the draw is the moment
        // credit is actually extended (`docs/05`).
        require(block.timestamp < q.validUntil, QuoteExpired());

        Terms memory t = quoteTerms[key];
        require(t.approvedPrincipal == drawAmount, AmountMismatch(t.approvedPrincipal, drawAmount));

        q.consumed = true;
        emit QuoteConsumedEvent(key, q.assessmentId);
        return t;
    }

    /// @notice May this binding take a risk-increasing action now?
    /// @dev Permission expires, and is void if the account has executed since
    /// it was issued — a state-version mismatch means the portfolio the
    /// permission was computed against no longer exists.
    function riskAllowed(Binding calldata b) external view returns (bool) {
        bytes32 key = bindingKey(b);
        if (riskPermission[key] != Permission.Allowed) return false;
        if (block.timestamp >= permissionValidUntil[key]) return false;
        if (permissionStateVersion[key] != b.accountStateVersion) return false;
        return true;
    }

    // --- EIP-712 ---

    function _hash(Assessment calldata a) private view returns (bytes32) {
        return keccak256(abi.encodePacked("\x19\x01", domainSeparator, _structHash(a)));
    }

    function _structHash(Assessment calldata a) private pure returns (bytes32) {
        // Split to stay clear of stack limits while keeping every
        // economically material field inside the signature.
        bytes32 head = keccak256(
            abi.encode(
                ASSESSMENT_TYPEHASH,
                a.deploymentId,
                a.evidencePolicyId,
                uint8(a.route),
                _bindingHash(a.binding),
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
                _termsHash(a.terms),
                a.loanId
            )
        );
        return keccak256(abi.encode(head, tail));
    }

    function _bindingHash(Binding calldata b) private pure returns (bytes32) {
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

    function _termsHash(Terms calldata t) private pure returns (bytes32) {
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

    function _recover(bytes32 digest, bytes calldata sig) private pure returns (address) {
        require(sig.length == 65, UnauthorizedSigner(address(0)));
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := calldataload(sig.offset)
            s := calldataload(add(sig.offset, 32))
            v := byte(0, calldataload(add(sig.offset, 64)))
        }
        return ecrecover(digest, v, r, s);
    }
}
