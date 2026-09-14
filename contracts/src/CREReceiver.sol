// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {
    Assessment, Binding, Terms, Route, Kind, Decision, Permission, Class
} from "./Assessment.sol";
import {RiskOracle} from "./RiskOracle.sol";

/// @notice The subset of ERC-165 a Keystone forwarder checks.
interface IERC165 {
    function supportsInterface(bytes4 interfaceId) external view returns (bool);
}

/// @notice What the KeystoneForwarder calls on a consumer.
interface IReceiver is IERC165 {
    /// @param metadata Report's metadata.
    /// @param report Workflow report.
    function onReport(bytes calldata metadata, bytes calldata report) external;
}

/// @notice What a CRE workflow may say about an open loan.
///
/// **Deliberately narrower than `Assessment`.** `docs/05`: a monitoring
/// result carries no request, grant, or terms and can never disburse. A
/// payload with no field for them cannot express one, so that rule is
/// enforced by the type rather than by a check that has to be remembered.
///
/// `accountStateVersion` comes from the REPORT, not from receiver storage:
/// it is the portfolio the engine actually assessed. `riskAllowedFor`
/// compares it against the account's current version, so permission issued
/// against a stale view is void by construction -- and a receiver that
/// substituted its own stored version would quietly defeat that.
struct MonitoringReport {
    uint256 loanId;
    uint64 accountStateVersion;
    uint64 sequence;
    uint64 validAfter;
    uint64 validUntil;
    Decision decision;
    Permission permission;
    Class class_;
    bytes32 assessmentId;
    bytes32 assessmentHash;
    bytes32 modelHash;
    bytes32 inputHash;
    bytes32 evidenceRoot;
}

/// @title CREReceiver
/// @notice Turns a Chainlink CRE report into an assessment the oracle can act on.
///
/// `docs/05`: "The CRE receiver verifies its expected forwarder and workflow
/// identity, then normalizes its report into the same typed fields." That
/// normalization is the whole job -- this contract decides nothing about
/// risk. It authenticates a report and hands the result to `RiskOracle`,
/// which applies exactly the rules a signed assessment goes through.
///
/// **Five things are checked before an assessment reaches the oracle**, and
/// each exists because skipping it makes the route meaningless:
///
///   1. `msg.sender` is the expected forwarder. Without this anyone calls
///      `onReport` directly and the DON is decorative.
///   2. The report's workflow owner matches the expected owner. A forwarder
///      routes for every workflow on the chain, so the forwarder alone does
///      not say WHOSE workflow produced this.
///   3. The workflow id matches. An owner may run several workflows; only
///      one of them underwrites.
///   4. The loan is one this receiver has been given a binding for. A
///      workflow cannot name an obligation the operator never enrolled,
///      and cannot supply the binding itself -- so it cannot rebind a loan
///      to an account or policy that was never assessed.
///   5. The report id is one this receiver has not seen. The forwarder may
///      retry a reverted delivery, and `docs/05` invariant 2 requires a
///      nonce consumed once.
///
/// Failing closed is the point: every one of these reverts, and a revert
/// means no assessment is recorded, which means no new risk is authorized.
contract CREReceiver is IReceiver {
    /// @dev Selector of the single function in IReceiver, which is also the
    /// ERC-165 interface id for a one-function interface.
    bytes4 private constant RECEIVER_INTERFACE_ID = IReceiver.onReport.selector;
    bytes4 private constant ERC165_INTERFACE_ID = type(IERC165).interfaceId;

    /// @dev The forwarder delivers 64 bytes: the 62-byte packed workflow
    /// identity, word-aligned, with the report id in the trailing two.
    /// Observed on Monad testnet rather than taken from documentation,
    /// which describes the 62-byte packing.
    uint256 private constant METADATA_LENGTH = 64;

    error UnexpectedSender(address caller, address expected);
    error UnexpectedWorkflowOwner(address owner, address expected);
    error UnexpectedWorkflow(bytes32 workflowId, bytes32 expected);
    error LoanNotEnrolled(uint256 loanId);
    error ReportAlreadySeen(bytes32 reportKey);
    error MalformedMetadata(uint256 length);
    error NotOperator();

    event LoanEnrolled(uint256 indexed loanId, uint256 indexed facilityId);
    event LoanRetired(uint256 indexed loanId);
    event ReportAccepted(
        bytes32 indexed reportKey, uint256 indexed loanId, uint64 sequence
    );

    /// @notice The only address permitted to deliver reports.
    address public immutable forwarder;
    /// @notice The workflow owner whose reports this receiver acts on.
    address public immutable workflowOwner;
    /// @notice The specific workflow whose reports this receiver acts on.
    bytes32 public immutable workflowId;
    /// @notice Where a normalized assessment is recorded.
    RiskOracle public immutable oracle;
    /// @notice Who may enrol a loan for monitoring.
    address public immutable operator;
    /// @notice The deployment this receiver speaks for.
    uint256 public immutable deploymentId;
    /// @notice The evidence policy assessments are made under.
    bytes32 public immutable evidencePolicyId;

    /// @dev The binding each enrolled loan is assessed against. Held here
    /// rather than taken from the report so a workflow cannot rebind a
    /// loan: everything identifying WHO is borrowing and under WHAT policy
    /// is the operator's to set, and only the risk answer comes from CRE.
    mapping(uint256 loanId => Binding) private enrolled;
    mapping(uint256 loanId => bool) public isEnrolled;

    /// @dev Consumed report ids. The forwarder retries a reverted delivery
    /// with more gas, so a receiver that acted twice on one report would
    /// let a single DON round advance the sequence twice.
    mapping(bytes32 reportKey => bool) public consumed;

    modifier onlyOperator() {
        require(msg.sender == operator, NotOperator());
        _;
    }

    constructor(
        address forwarder_,
        address workflowOwner_,
        bytes32 workflowId_,
        RiskOracle oracle_,
        address operator_,
        uint256 deploymentId_,
        bytes32 evidencePolicyId_
    ) {
        forwarder = forwarder_;
        workflowOwner = workflowOwner_;
        workflowId = workflowId_;
        oracle = oracle_;
        operator = operator_;
        deploymentId = deploymentId_;
        evidencePolicyId = evidencePolicyId_;
    }

    /// @notice Register the binding a loan is monitored under.
    /// @dev The workflow names a loan; the operator says what that loan
    /// IS. Splitting it this way means a compromised workflow can withdraw
    /// permission or refuse it, but cannot invent an obligation or move an
    /// existing one to a different account.
    function enrol(uint256 loanId, Binding calldata binding)
        external
        onlyOperator
    {
        enrolled[loanId] = binding;
        isEnrolled[loanId] = true;
        emit LoanEnrolled(loanId, binding.facilityId);
    }

    /// @notice Stop accepting reports for a resolved loan.
    /// @dev Kept open to the operator alone, and always available: removing
    /// a route only ever reduces what can be authorized.
    function retire(uint256 loanId) external onlyOperator {
        delete enrolled[loanId];
        isEnrolled[loanId] = false;
        emit LoanRetired(loanId);
    }

    function bindingFor(uint256 loanId) external view returns (Binding memory) {
        return enrolled[loanId];
    }

    /// @notice The sequence a report for this loan must exceed.
    ///
    /// @dev `docs/05` requires a single strictly increasing sequence per
    /// binding across BOTH assessment kinds, so the workflow cannot simply
    /// count its own runs -- an origination on the signed path advances
    /// the same counter. Reading it here means the workflow asks the
    /// authority rather than guessing, and a guess that collided would
    /// have its update rejected and the permission left to expire.
    ///
    /// Returns zero for a loan that is not enrolled: a caller that treated
    /// that as "start at 1" would be writing for an obligation this
    /// receiver will refuse anyway.
    function nextSequence(uint256 loanId) external view returns (uint64) {
        if (!isEnrolled[loanId]) return 0;
        return oracle.lastSequence(oracle.bindingKey(enrolled[loanId])) + 1;
    }

    /// @inheritdoc IReceiver
    function onReport(bytes calldata metadata, bytes calldata report)
        external
    {
        // 1. Only the forwarder. Checked first and cheaply: everything
        // after this trusts the caller to have run DON consensus.
        require(msg.sender == forwarder, UnexpectedSender(msg.sender, forwarder));

        (bytes32 reportWorkflowId, address reportOwner, bytes2 reportId) =
            _decodeMetadata(metadata);

        // 2 and 3. The forwarder routes for every workflow on the chain, so
        // it attests that SOME workflow produced this, not which one.
        require(
            reportOwner == workflowOwner,
            UnexpectedWorkflowOwner(reportOwner, workflowOwner)
        );
        require(
            reportWorkflowId == workflowId,
            UnexpectedWorkflow(reportWorkflowId, workflowId)
        );

        MonitoringReport memory r = abi.decode(report, (MonitoringReport));

        // 4. Only a loan the operator enrolled.
        require(isEnrolled[r.loanId], LoanNotEnrolled(r.loanId));

        // 5. One effect per report. The report id alone is two bytes and
        // repeats across workflows, so the key commits to the workflow and
        // the assessment as well -- a receiver keyed on reportId alone
        // would refuse an unrelated later report that happened to reuse it.
        bytes32 reportKey =
            keccak256(abi.encode(reportWorkflowId, reportId, r.assessmentId));
        require(!consumed[reportKey], ReportAlreadySeen(reportKey));
        consumed[reportKey] = true;

        // The oracle applies every rule a signed assessment goes through:
        // deployment, route, expiry, sequence, field shape. This contract
        // adds authentication, never permission.
        oracle.acceptFromRoute(_normalize(r));

        emit ReportAccepted(reportKey, r.loanId, r.sequence);
    }

    /// @inheritdoc IERC165
    function supportsInterface(bytes4 interfaceId)
        external
        pure
        returns (bool)
    {
        return interfaceId == RECEIVER_INTERFACE_ID
            || interfaceId == ERC165_INTERFACE_ID;
    }

    /// @dev Expand a monitoring report into the assessment the oracle reads.
    ///
    /// The grant fields are zero because a monitoring result has nowhere to
    /// put them -- `docs/05` forbids them, and the oracle rejects them, but
    /// the reason they are absent here is that `MonitoringReport` has no
    /// field to carry one.
    function _normalize(MonitoringReport memory r)
        private
        view
        returns (Assessment memory a)
    {
        Binding memory b = enrolled[r.loanId];
        // The version the ENGINE assessed, not one this contract stored.
        // A stale version would be compared against the account's current
        // one by `riskAllowedFor` and correctly fail; substituting a
        // stored value would hide that.
        b.accountStateVersion = r.accountStateVersion;

        a = Assessment({
            deploymentId: deploymentId,
            evidencePolicyId: evidencePolicyId,
            route: Route.CRE,
            binding: b,
            kind: Kind.Monitoring,
            assessmentId: r.assessmentId,
            assessmentHash: r.assessmentHash,
            decision: r.decision,
            permission: r.permission,
            modelHash: r.modelHash,
            inputHash: r.inputHash,
            evidenceRoot: r.evidenceRoot,
            class_: r.class_,
            sequence: r.sequence,
            validAfter: r.validAfter,
            validUntil: r.validUntil,
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
            loanId: r.loanId
        });
    }

    /// @dev Metadata is `abi.encodePacked(workflowId, workflowName,
    /// workflowOwner)` -- 62 bytes -- delivered word-aligned as 64, with
    /// the report id in the final two.
    ///
    /// The length is checked rather than assumed. Reading fixed offsets out
    /// of a shorter slice does not revert in calldata; it reads whatever
    /// follows, so a malformed delivery would silently produce a workflow
    /// owner that happens to match.
    function _decodeMetadata(bytes calldata metadata)
        private
        pure
        returns (bytes32 id, address owner, bytes2 reportId)
    {
        require(
            metadata.length == METADATA_LENGTH,
            MalformedMetadata(metadata.length)
        );
        id = bytes32(metadata[0:32]);
        // workflowName occupies [32:42] and is not used: the id already
        // identifies the workflow, and the name is mutable.
        owner = address(bytes20(metadata[42:62]));
        reportId = bytes2(metadata[62:64]);
    }
}
