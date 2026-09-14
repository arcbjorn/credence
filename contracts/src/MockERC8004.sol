// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @title MockIdentityRegistry
/// @notice The subset of the ERC-8004 identity registry Credence reads.
///
/// **A test double, not an implementation.** It exists so the publishing
/// path can be exercised on a local chain, where no canonical registry is
/// deployed. `docs/08` permits a local stand-in for development and is
/// explicit that it does not satisfy the release row, which requires the
/// selected network's real registry.
///
/// The signatures match the deployed contract exactly — verified against
/// Monad testnet on 14 September — because a double with a convenient ABI
/// would let a call pass here and revert against the real thing.
contract MockIdentityRegistry {
    error ERC721NonexistentToken(uint256 tokenId);

    mapping(uint256 tokenId => address) private owners;
    uint256 private nextId = 1;

    event Registered(uint256 indexed agentId, address indexed owner);

    /// @notice Mint an agent identity to the caller.
    function register(string calldata) external returns (uint256 agentId) {
        agentId = nextId++;
        owners[agentId] = msg.sender;
        emit Registered(agentId, msg.sender);
    }

    /// @notice Mint to a chosen owner, so a test can separate the agent
    /// from the client without juggling keys.
    function registerTo(address owner, uint256 agentId) external {
        owners[agentId] = owner;
        emit Registered(agentId, owner);
    }

    /// @dev Reverts for an unregistered id, exactly as the ERC-721 the
    /// real registry is built on does. Returning address(0) instead would
    /// let a caller treat "no such agent" as "unowned agent".
    function ownerOf(uint256 agentId) external view returns (address) {
        address owner = owners[agentId];
        require(owner != address(0), ERC721NonexistentToken(agentId));
        return owner;
    }
}

/// @title MockReputationRegistry
/// @notice The subset of the ERC-8004 reputation registry Credence uses.
///
/// Reproduces three behaviours the real contract has that a naive double
/// would miss, each of which Credence depends on:
///
///   1. **No idempotency.** `feedbackIndex` auto-increments per
///      (agentId, clientAddress). The same outcome sent twice becomes two
///      entries, so the client must dedupe before sending.
///   2. **Self-feedback is refused.** An agent's owner cannot report on
///      itself, which is why Credence publishes as a separate client.
///   3. **feedbackURI and feedbackHash are write-only.** Neither read
///      function returns them, so a reader cannot key on them — which is
///      why the loan id rides in tag2.
contract MockReputationRegistry {
    struct Feedback {
        address client;
        uint64 index;
        int128 value;
        uint8 valueDecimals;
        string tag1;
        string tag2;
        bool revoked;
    }

    MockIdentityRegistry public immutable identity;

    mapping(uint256 agentId => Feedback[]) private entries;
    mapping(uint256 agentId => mapping(address client => uint64)) private counts;

    event NewFeedback(
        uint256 indexed agentId,
        address indexed clientAddress,
        uint64 feedbackIndex,
        int128 value,
        uint8 valueDecimals,
        string indexed indexedTag1,
        string tag1,
        string tag2,
        string endpoint,
        string feedbackURI,
        bytes32 feedbackHash
    );

    constructor(MockIdentityRegistry identityRegistry) {
        identity = identityRegistry;
    }

    function giveFeedback(
        uint256 agentId,
        int128 value,
        uint8 valueDecimals,
        string calldata tag1,
        string calldata tag2,
        string calldata endpoint,
        string calldata feedbackURI,
        bytes32 feedbackHash
    ) external {
        // The real registry refuses this, and Credence's whole client
        // topology follows from it: the borrowing agent must be owned by
        // someone other than the publisher.
        require(identity.ownerOf(agentId) != msg.sender, "Self-feedback not allowed");

        uint64 index = ++counts[agentId][msg.sender];
        entries[agentId].push(
            Feedback({
                client: msg.sender,
                index: index,
                value: value,
                valueDecimals: valueDecimals,
                tag1: tag1,
                tag2: tag2,
                revoked: false
            })
        );

        emit NewFeedback(
            agentId,
            msg.sender,
            index,
            value,
            valueDecimals,
            tag1,
            tag1,
            tag2,
            endpoint,
            feedbackURI,
            feedbackHash
        );
    }

    /// @dev Note what is absent: feedbackURI and feedbackHash. The real
    /// registry does not return them either, which is the constraint that
    /// puts the loan id in tag2.
    function readAllFeedback(
        uint256 agentId,
        address[] calldata clientAddresses,
        string calldata tag1,
        string calldata tag2,
        bool includeRevoked
    )
        external
        view
        returns (
            address[] memory clients,
            uint64[] memory feedbackIndexes,
            int128[] memory values,
            uint8[] memory valueDecimals,
            string[] memory tag1s,
            string[] memory tag2s,
            bool[] memory revokedStatuses
        )
    {
        Feedback[] storage all = entries[agentId];

        uint256 n;
        for (uint256 i = 0; i < all.length; i++) {
            if (_matches(all[i], clientAddresses, tag1, tag2, includeRevoked)) {
                n++;
            }
        }

        clients = new address[](n);
        feedbackIndexes = new uint64[](n);
        values = new int128[](n);
        valueDecimals = new uint8[](n);
        tag1s = new string[](n);
        tag2s = new string[](n);
        revokedStatuses = new bool[](n);

        uint256 j;
        for (uint256 i = 0; i < all.length; i++) {
            if (!_matches(all[i], clientAddresses, tag1, tag2, includeRevoked)) {
                continue;
            }
            clients[j] = all[i].client;
            feedbackIndexes[j] = all[i].index;
            values[j] = all[i].value;
            valueDecimals[j] = all[i].valueDecimals;
            tag1s[j] = all[i].tag1;
            tag2s[j] = all[i].tag2;
            revokedStatuses[j] = all[i].revoked;
            j++;
        }
    }

    function _matches(
        Feedback storage f,
        address[] calldata clientAddresses,
        string calldata tag1,
        string calldata tag2,
        bool includeRevoked
    ) private view returns (bool) {
        if (f.revoked && !includeRevoked) return false;
        // An empty filter means "every client", matching the real
        // contract: passing no addresses must not match nothing.
        if (clientAddresses.length > 0) {
            bool found;
            for (uint256 i = 0; i < clientAddresses.length; i++) {
                if (clientAddresses[i] == f.client) {
                    found = true;
                    break;
                }
            }
            if (!found) return false;
        }
        if (bytes(tag1).length > 0 && !_eq(f.tag1, tag1)) return false;
        if (bytes(tag2).length > 0 && !_eq(f.tag2, tag2)) return false;
        return true;
    }

    function _eq(string storage a, string calldata b) private pure returns (bool) {
        return keccak256(bytes(a)) == keccak256(bytes(b));
    }
}
