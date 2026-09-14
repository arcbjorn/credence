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
        // Commitment to the COMPLETE signed binding. The standing-quote
        // key covers only the stable identity (registry, agent, account,
        // facility), so without this the caller could present the same key
        // with rewritten operator, configuration, or policy fields -- or a
        // bumped state version -- and still draw on this quote.
        bytes32 bindingCommitment;
    }

    address public immutable operator;
    uint256 public immutable deploymentId;

    /// @dev The class this deployment may execute.
    ///
    /// `docs/04`: scenario fallback starts a SEPARATELY BOUND scenario
    /// deployment. Its assessments are honestly ScenarioOnly, and that
    /// deployment must be able to act on them -- while a production
    /// deployment still refuses anything below PrototypeEligible. Pinned
    /// at construction so it cannot be widened later.
    Class public immutable executableClass;
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
    /// @dev Commitment to the complete binding a monitoring permission was
    /// issued for, so a permission cannot be read against a binding that
    /// merely shares the standing key.
    mapping(bytes32 bindingKey => bytes32) public permissionBinding;
    /// @dev Live obligations per facility. Configuration is immutable
    /// while this is non-zero (`docs/05`).
    mapping(uint256 facilityId => uint256) public liveLoans;

    error NotOperator();
    /// @dev The loan manager is wired once; re-pointing it mid-epoch would
    /// redirect origination and recovery away from the contract lenders'
    /// obligations were written against.
    error LoanManagerAlreadySet();
    error LoanManagerRequired();
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
    error BindingMismatch();
    error ContradictoryPermission();
    error FacilityHasLiveObligations(uint256 facilityId);
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

    constructor(address operator_, uint256 deploymentId_, Class executableClass_) {
        operator = operator_;
        deploymentId = deploymentId_;
        executableClass = executableClass_;
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

    /// @notice Wire the loan manager. Callable once.
    ///
    /// @dev One-shot, not merely operator-gated. This pointer decides which
    /// contract may open debt, consume quotes, and drive recovery, so an
    /// operator able to re-point it mid-epoch could redirect every one of
    /// those to a contract lenders never agreed to -- with existing
    /// obligations still outstanding against the old one.
    ///
    /// A deployment wires itself once and the address becomes immutable in
    /// practice. `docs/05` calls the system non-upgradeable: a changed
    /// implementation gets a new deployment manifest and a new assessment
    /// domain, which is exactly what this enforces rather than assumes.
    ///
    /// Reported by review, 14 September.
    function setLoanManager(address loanManager_) external onlyOperator {
        require(loanManager == address(0), LoanManagerAlreadySet());
        require(loanManager_ != address(0), LoanManagerRequired());
        loanManager = loanManager_;
    }

    /// @notice Pin a facility's authorization route for its epoch.
    /// @dev `docs/05`: changing an existing facility's route requires a new
    /// epoch after its obligations resolve. Re-pinning under a live loan
    /// would change the rules governing an obligation already struck --
    /// the borrower agreed to terms under one authorization domain and
    /// would find itself under another.
    function pinRoute(uint256 facilityId, Route route) external onlyOperator {
        require(liveLoans[facilityId] == 0, FacilityHasLiveObligations(facilityId));
        facilityRoute[facilityId] = route;
        emit RoutePinned(facilityId, route);
    }

    /// @notice Record that a facility has one more live obligation.
    /// @dev Loan-manager only. The oracle needs this to enforce epoch
    /// immutability without reaching into the manager on every read.
    function openObligation(uint256 facilityId) external {
        require(msg.sender == loanManager, NotConsumer());
        liveLoans[facilityId] += 1;
    }

    /// @notice Record that an obligation resolved.
    /// @dev Saturating rather than checked: a double-close must not brick
    /// the facility, and the count only ever gates configuration changes.
    function closeObligation(uint256 facilityId) external {
        require(msg.sender == loanManager, NotConsumer());
        if (liveLoans[facilityId] > 0) {
            liveLoans[facilityId] -= 1;
        }
    }

    function setSigner(Route route, address signer, bool allowed) external onlyOperator {
        routeSigner[route][signer] = allowed;
        emit SignerSet(route, signer, allowed);
    }

    /// @notice The EIP-712 hash of a complete binding.
    /// @dev Public so the loan manager can pin exactly what was signed
    /// onto the credit account at origination.
    function bindingHash(Binding calldata b) external pure returns (bytes32) {
        return _bindingHash(b);
    }

    /// @notice The binding's IMMUTABLE identity: everything a monitoring
    /// permission must match, excluding the account state version.
    ///
    /// The state version changes on every execution and is checked
    /// separately, against the account's live value. Folding it into the
    /// commitment too would mean a correctly signed monitoring result
    /// could never match after the first action -- the account would be
    /// permanently unable to act (`docs/05`).
    /// @dev Whether a class may move capital in THIS deployment.
    ///
    /// An exact match, not a threshold: ObservedShadow is not "less
    /// executable" than PrototypeEligible, it is a different kind of
    /// record, and a scenario deployment must not execute shadow results
    /// any more than a production one executes scenario results.
    function _isExecutable(Class c) private view returns (bool) {
        return c == executableClass;
    }

    function bindingConfigHash(Binding calldata b) public pure returns (bytes32) {
        return keccak256(
            abi.encode(
                b.identityRegistryChainId,
                b.identityRegistry,
                b.agentId,
                b.operator,
                b.creditAccount,
                b.facilityId,
                b.configVersion,
                b.configHash,
                b.policyEpoch,
                b.policyHash
            )
        );
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
        Route pinned = _pinnedRoute(a);

        address signer = _recover(_hash(a), signature);
        require(routeSigner[pinned][signer], UnauthorizedSigner(signer));

        _record(a);
    }

    /// @notice Accept an assessment authenticated by the CALLER rather than
    /// by a signature.
    ///
    /// @dev For the CRE route, where a report arrives through the
    /// KeystoneForwarder and carries no ECDSA signature to recover. The
    /// DON's consensus is the authentication, and the receiver that
    /// verified the forwarder is what this trusts -- so the caller must be
    /// registered for the facility's pinned route exactly as a signer
    /// would be.
    ///
    /// Every other check is shared with `accept`: this is deliberately not
    /// a second implementation, because a CRE path that drifted from the
    /// signed path would enforce a different set of rules than the one
    /// `docs/05` describes.
    ///
    /// `docs/05` invariant 6: a CRE facility rejects development
    /// attestations. That falls out of the same `routeSigner` check --
    /// a development key is not registered under `Route.CRE`, and this
    /// receiver is not registered under `Route.Development`.
    function acceptFromRoute(Assessment calldata a) external {
        Route pinned = _pinnedRoute(a);
        require(routeSigner[pinned][msg.sender], UnauthorizedSigner(msg.sender));
        _record(a);
    }

    function _pinnedRoute(Assessment calldata a) private view returns (Route) {
        require(a.deploymentId == deploymentId, WrongDeployment());
        Route pinned = facilityRoute[a.binding.facilityId];
        require(pinned != Route.Unset, RouteNotPinned(a.binding.facilityId));
        require(a.route == pinned, WrongRoute(pinned, a.route));
        return pinned;
    }

    /// @dev Everything an accepted assessment does, independent of how it
    /// was authenticated.
    function _record(Assessment calldata a) private {
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
            // A newer origination result supersedes any standing
            // monitoring permission. A denial is new information about
            // this binding, so leaving an older Allowed in place would let
            // the account keep acting on a superseded view.
            if (a.decision != Decision.Approved || !_isExecutable(a.class_)) {
                delete riskPermission[key];
                delete permissionStateVersion[key];
                delete permissionValidUntil[key];
                delete permissionBinding[key];
            }
            if (a.decision == Decision.Approved && _isExecutable(a.class_)) {
                quote[key] = Stored({
                    assessmentId: a.assessmentId,
                    sequence: a.sequence,
                    consumed: false,
                    validUntil: a.validUntil,
                    bindingCommitment: _bindingHash(a.binding)
                });
                quoteTerms[key] = a.terms;
            }
        } else {
            // One transition rule across both kinds. Permission only ever
            // stands on an EXECUTABLE monitoring approval; everything else
            // -- a shadow class, a denial, an unavailable -- clears it.
            //
            // Clearing rather than ignoring matters: a newer assessment
            // that cannot grant permission must still revoke whatever
            // stood before, or a stale Allowed survives the very update
            // that was meant to withdraw it.
            bool grants = a.decision == Decision.Approved
                && a.permission == Permission.Allowed
                && _isExecutable(a.class_);

            if (grants) {
                riskPermission[key] = Permission.Allowed;
                permissionStateVersion[key] = a.binding.accountStateVersion;
                permissionValidUntil[key] = a.validUntil;
                    permissionBinding[key] = bindingConfigHash(a.binding);
            } else {
                delete riskPermission[key];
                delete permissionStateVersion[key];
                delete permissionValidUntil[key];
                delete permissionBinding[key];
            }
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
            // Decision and permission are separate fields but not
            // independent ones: a Denied result that claims Allowed is a
            // contradiction, not a permissive edge case (`docs/04`).
            require(
                a.decision == Decision.Approved || a.permission == Permission.Blocked,
                ContradictoryPermission()
            );
        } else if (a.decision != Decision.Approved) {
            require(!hasGrant, GrantOnNonApproval());
            require(a.permission == Permission.Blocked, ContradictoryPermission());
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
    /// @return t The signed terms.
    /// @return validUntil When the authorizing quote expires.
    /// @return sequence The assessment sequence this quote was signed at,
    /// so a consumer can tell whether a NEWER assessment has since
    /// superseded it. Returned so
    /// the loan manager can bound the action it authorized: `docs/05`
    /// covers the initial atomic action by the consumed quote, and a quote
    /// that has expired covers nothing.
    function consumeQuote(Binding calldata b, uint256 drawAmount)
        external
        returns (Terms memory t, uint64 validUntil, uint64 sequence)
    {
        require(msg.sender == loanManager, NotConsumer());
        bytes32 key = bindingKey(b);
        Stored storage q = quote[key];
        require(q.assessmentId != bytes32(0), NoQuote());
        require(!q.consumed, QuoteConsumed());
        // Validity binds here, not only at accept: the draw is the moment
        // credit is actually extended (`docs/05`).
        require(block.timestamp < q.validUntil, QuoteExpired());
        // The binding presented here must be the one that was signed, in
        // full -- not merely one that shares the standing-quote key.
        require(_bindingHash(b) == q.bindingCommitment, BindingMismatch());

        t = quoteTerms[key];
        require(t.approvedPrincipal == drawAmount, AmountMismatch(t.approvedPrincipal, drawAmount));
        validUntil = q.validUntil;
        sequence = q.sequence;

        q.consumed = true;
        emit QuoteConsumedEvent(key, q.assessmentId);
    }

    /// @notice May the binding a credit account is bound to act now?
    ///
    /// Keyed by the standing-quote key with an explicit commitment to the
    /// complete binding, so a caller cannot present a different binding
    /// that happens to share the key. Exists because a credit account
    /// holds a hash, not the struct: making it reconstruct one would mean
    /// trusting fields the caller chose.
    function riskAllowedFor(bytes32 key, bytes32 bindingCommitment, uint64 stateVersion)
        external
        view
        returns (bool)
    {
        if (permissionBinding[key] != bindingCommitment) return false;
        if (riskPermission[key] != Permission.Allowed) return false;
        if (block.timestamp >= permissionValidUntil[key]) return false;
        if (permissionStateVersion[key] != stateVersion) return false;
        return true;
    }

    /// @notice May this binding take a risk-increasing action now?
    /// @dev Permission expires, and is void if the account has executed since
    /// it was issued — a state-version mismatch means the portfolio the
    /// permission was computed against no longer exists.
    /// @notice Was permission WITHDRAWN, as opposed to merely lapsed?
    ///
    /// @dev The two are different facts and only one of them is a
    /// statement about the borrower.
    ///
    /// An explicit denial clears the record, including `validUntil`, so a
    /// zero there means the last thing the attestor said was "blocked" --
    /// or that nothing was ever said. A non-zero `validUntil` in the past
    /// means the opposite: the last thing said was "allowed", and it
    /// simply ran out because no newer assessment arrived.
    ///
    /// Recovery uses this rather than `riskAllowed` so that an attestor
    /// who goes SILENT cannot make a healthy, unmatured loan seizable by
    /// doing nothing. Without the distinction the borrower's covenant is
    /// "keep the attestor answering", which is not the instrument they
    /// agreed to (`docs/05` invariant 12: recovery needs a trigger).
    ///
    /// A lapse still blocks new risk -- `riskAllowed` is unchanged, and
    /// `docs/05` requires expired permission to prevent risk increases.
    /// It just is not, by itself, grounds to seize.
    function permissionWithdrawn(Binding calldata b)
        external
        view
        returns (bool)
    {
        bytes32 key = bindingKey(b);
        if (riskPermission[key] == Permission.Allowed
            && permissionValidUntil[key] > block.timestamp
            && permissionStateVersion[key] == b.accountStateVersion) {
            // Currently allowed: nothing was withdrawn.
            return false;
        }
        // Withdrawn, or never granted. Both leave no standing window.
        return permissionValidUntil[key] == 0;
    }

    /// @notice When the standing permission for this binding lapses.
    /// @dev Zero when none stands. Read by the loan manager so a lapse can
    /// be given a grace period rather than treated as a withdrawal.
    function permissionExpiry(Binding calldata b)
        external
        view
        returns (uint64)
    {
        return permissionValidUntil[bindingKey(b)];
    }

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
        // Reject the high-s half of each signature pair.
        //
        // secp256k1 admits two valid signatures per message, (r, s) and
        // (r, n - s), so a third party can produce a second well-formed
        // signature over an assessment they did not author. Sequence
        // monotonicity already stops that being replayable here, but a
        // signature check that accepts both forms is a weaker statement
        // than one that does not -- and this is the check every route's
        // authority rests on. Reported by review, 14 September.
        require(
            uint256(s) <= 0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF5D576E7357A4501DDFE92F46681B20A0,
            UnauthorizedSigner(address(0))
        );
        require(v == 27 || v == 28, UnauthorizedSigner(address(0)));

        address signer = ecrecover(digest, v, r, s);
        // ecrecover returns zero on failure rather than reverting, and a
        // zero address must never match an allowlisted signer.
        require(signer != address(0), UnauthorizedSigner(address(0)));
        return signer;
    }
}
