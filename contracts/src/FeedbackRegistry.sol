// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @title FeedbackRegistry
/// @notice Attributable obligation outcomes, published for anyone to read.
///
/// `docs/06`: a third party must be able to read an agent's history
/// WITHOUT asking Credence. That rules out an API — the data has to live
/// on chain, keyed by the identity tuple, in a shape a reader can decode
/// from the ABI alone.
///
/// **This is a local adapter.** `docs/08` permits one for development and
/// is explicit that it does not satisfy the release identity gate, which
/// requires the selected network's ERC-8004-compatible registry. The shape
/// here deliberately mirrors what that registry records, so the reader
/// built against it ports rather than being rewritten.
///
/// Four signals stay separate (`docs/06`): a repayment, a profitable
/// strategy, a reliable forecast, and absence of default are different
/// facts and are never combined into one.
contract FeedbackRegistry {
    /// @dev Outcome classes. Deliberately not a score: collapsing these
    /// into one number is the thing `docs/06` forbids.
    enum Outcome {
        Repaid,
        Recovered,
        Defaulted
    }

    /// @notice One obligation's resolved outcome.
    /// @dev Amounts and durations are recorded as observed, so a reader
    /// can recompute any aggregate rather than trusting one we chose.
    struct Feedback {
        uint256 agentId;
        address creditAccount;
        uint256 loanId;
        Outcome outcome;
        uint256 principal;
        uint256 interestPaid;
        uint256 principalLoss;
        uint64 originatedAt;
        uint64 resolvedAt;
        /// @dev Hash of the retained evidence this outcome came from, so a
        /// reader can demand the record rather than take the claim.
        bytes32 evidenceRef;
    }

    address public immutable publisher;

    /// @dev Append-only. A correction is a new entry, never a rewrite
    /// (`docs/06`).
    Feedback[] public entries;

    /// @dev Entry indices per agent, so a reader can enumerate one agent
    /// without scanning everything.
    mapping(uint256 agentId => uint256[]) private byAgent;

    /// @dev Idempotency: the same loan is published once. A queue that
    /// retries must not double-count an outcome.
    mapping(bytes32 key => bool) public published;

    error NotPublisher();
    error AlreadyPublished(uint256 loanId);

    event FeedbackPublished(
        uint256 indexed agentId,
        address indexed creditAccount,
        uint256 indexed loanId,
        Outcome outcome,
        uint256 index
    );

    constructor(address publisher_) {
        publisher = publisher_;
    }

    /// @notice Publish one resolved obligation.
    function publish(Feedback calldata f) external returns (uint256 index) {
        require(msg.sender == publisher, NotPublisher());

        bytes32 key = keccak256(abi.encode(f.creditAccount, f.loanId));
        require(!published[key], AlreadyPublished(f.loanId));
        published[key] = true;

        index = entries.length;
        entries.push(f);
        byAgent[f.agentId].push(index);

        emit FeedbackPublished(f.agentId, f.creditAccount, f.loanId, f.outcome, index);
    }

    function entryCount() external view returns (uint256) {
        return entries.length;
    }

    /// @notice Every entry index for one agent.
    function agentEntries(uint256 agentId) external view returns (uint256[] memory) {
        return byAgent[agentId];
    }

    /// @notice One entry, whole.
    /// @dev Returned as a struct rather than as a summary: a reader
    /// computes its own aggregates from the underlying facts.
    function entryAt(uint256 index) external view returns (Feedback memory) {
        return entries[index];
    }
}
