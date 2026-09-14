// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Assessment, Route} from "./Assessment.sol";
import {RiskOracle} from "./RiskOracle.sol";

/// @notice The subset of ERC-165 a Keystone forwarder checks.
interface IERC165 {
    function supportsInterface(bytes4 interfaceId) external view returns (bool);
}

/// @notice What the KeystoneForwarder calls on a consumer.
interface IReceiver is IERC165 {
    /// @param metadata Report metadata: workflow identity, then report id.
    /// @param report The workflow's encoded output.
    function onReport(bytes calldata metadata, bytes calldata report) external;
}

/// @title CREReceiver
/// @notice Turns a Chainlink CRE report into an assessment the oracle can act on.
///
/// `docs/05`: "The CRE receiver verifies its expected forwarder and workflow
/// identity, then normalizes its report into the same typed fields." That
/// normalization is the whole job — this contract decides nothing about
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
///   4. The assessment's route is `CRE`. A report carrying a
///      `Development` route is a malformed message, not a fallback.
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
    uint256 private constant METADATA_LENGTH = 64;

    error UnexpectedSender(address caller, address expected);
    error UnexpectedWorkflowOwner(address owner, address expected);
    error UnexpectedWorkflow(bytes32 workflowId, bytes32 expected);
    error NotCRERoute(Route route);
    error ReportAlreadySeen(bytes32 reportKey);
    error MalformedMetadata(uint256 length);

    event ReportAccepted(
        bytes32 indexed reportKey, bytes32 indexed workflowId, uint64 sequence
    );

    /// @notice The only address permitted to deliver reports.
    address public immutable forwarder;
    /// @notice The workflow owner whose reports this receiver acts on.
    address public immutable workflowOwner;
    /// @notice The specific workflow whose reports this receiver acts on.
    bytes32 public immutable workflowId;
    /// @notice Where a normalized assessment is recorded.
    RiskOracle public immutable oracle;

    /// @dev Consumed report ids. The forwarder retries a reverted delivery
    /// with more gas, so a receiver that acted twice on one report would
    /// let a single DON round advance the sequence twice.
    mapping(bytes32 reportKey => bool) public consumed;

    constructor(
        address forwarder_,
        address workflowOwner_,
        bytes32 workflowId_,
        RiskOracle oracle_
    ) {
        forwarder = forwarder_;
        workflowOwner = workflowOwner_;
        workflowId = workflowId_;
        oracle = oracle_;
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

        Assessment memory a = abi.decode(report, (Assessment));

        // 4. A report claiming the development route is malformed. The
        // oracle would reject it anyway for a CRE facility, but refusing
        // here keeps the failure attributable to the report rather than
        // surfacing as a route mismatch further away.
        require(a.route == Route.CRE, NotCRERoute(a.route));

        // 5. One effect per report. The report id alone is two bytes and
        // repeats across workflows, so the key commits to the workflow and
        // the assessment as well -- a receiver keyed on reportId alone
        // would refuse an unrelated later report that happened to reuse it.
        bytes32 reportKey =
            keccak256(abi.encode(reportWorkflowId, reportId, a.assessmentId));
        require(!consumed[reportKey], ReportAlreadySeen(reportKey));
        consumed[reportKey] = true;

        // The oracle applies every rule a signed assessment goes through:
        // deployment, route, expiry, sequence, field shape. This contract
        // adds authentication, never permission.
        oracle.acceptFromRoute(a);

        emit ReportAccepted(reportKey, reportWorkflowId, a.sequence);
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
