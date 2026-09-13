// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @notice Independent valuation for the supported risky asset.
///
/// Separate from the venue on purpose. `docs/05`: slippage must be checked
/// against a permitted valuation source, not only a caller-supplied
/// `minOut` — a venue that quotes its own fair price cannot detect a trade
/// that transfers value to a cooperating counterparty.
interface IPriceSource {
    /// @return price Settlement base units per whole risky unit.
    /// @return scale Decimal scale of `price`.
    /// @return observedAt Timestamp of the observation, so staleness is
    /// the caller's to judge rather than silently assumed acceptable.
    function riskyPrice() external view returns (uint256 price, uint32 scale, uint64 observedAt);
}
