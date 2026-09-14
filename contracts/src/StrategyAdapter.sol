// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {IPriceSource} from "./IPriceSource.sol";
import {SafeTransfer} from "./SafeTransfer.sol";
import {SpotVenue} from "./SpotVenue.sol";

/// @title StrategyAdapter
/// @notice The one verified execution route for the prototype.
///
/// `docs/05`: one verified route, fixed receiver, bounded approvals,
/// slippage and output checks.
///
/// The guard that matters is the slippage check. A whitelisted venue is
/// **not** sufficient: a venue quoting a bad price transfers value to a
/// cooperating counterparty, and a `minOut` supplied by the caller only
/// protects a caller who is not the attacker. So every trade is measured
/// against an independent price source, and rejected if it deviates beyond
/// a bounded tolerance — regardless of what the venue or the caller says.
///
/// The adapter holds nothing. It pulls from the calling account, trades,
/// and returns output to that same account, so there is no balance for a
/// later call to sweep.
contract StrategyAdapter {
    using SafeTransfer for IERC20;

    IERC20 public immutable settlement;
    IERC20 public immutable risky;
    SpotVenue public immutable venue;
    IPriceSource public immutable priceSource;

    /// @dev Maximum deviation from the independent price, in basis points.
    uint32 public immutable maxSlippageBps;
    /// @dev Oldest acceptable price observation.
    uint64 public immutable maxPriceAgeSeconds;
    uint32 public immutable riskyDecimals;

    error SlippageExceeded(uint256 expected, uint256 actual, uint32 toleranceBps);
    error PriceStale(uint64 observedAt, uint64 maxAge);
    error PriceUnavailable();
    error NothingReceived();

    event Acquired(address indexed account, uint256 settlementIn, uint256 riskyOut);
    event Unwound(address indexed account, uint256 riskyIn, uint256 settlementOut);

    constructor(
        IERC20 settlement_,
        IERC20 risky_,
        SpotVenue venue_,
        IPriceSource priceSource_,
        uint32 maxSlippageBps_,
        uint64 maxPriceAgeSeconds_,
        uint32 riskyDecimals_
    ) {
        settlement = settlement_;
        risky = risky_;
        venue = venue_;
        priceSource = priceSource_;
        maxSlippageBps = maxSlippageBps_;
        maxPriceAgeSeconds = maxPriceAgeSeconds_;
        riskyDecimals = riskyDecimals_;
    }

    /// @notice Convert settlement into the risky asset.
    /// @dev Called by a credit account, so `msg.sender` is that account and
    /// both the pull and the return target it. There is no recipient
    /// parameter: output cannot be redirected.
    function acquire(uint256 settlementIn) external returns (uint256 riskyOut) {
        address account = msg.sender;
        settlement.safeTransferFrom(account, address(this), settlementIn);

        // Approve exactly what this trade needs, and clear it afterwards so
        // no standing allowance survives the call.
        settlement.approve(address(venue), settlementIn);
        riskyOut = venue.buy(settlementIn);
        settlement.approve(address(venue), 0);

        require(riskyOut > 0, NothingReceived());
        _requireFairBuy(settlementIn, riskyOut);

        risky.safeTransfer(account, riskyOut);
        emit Acquired(account, settlementIn, riskyOut);
    }

    /// @notice Convert the risky asset back into settlement.
    function unwind(uint256 riskyIn) external returns (uint256 settlementOut) {
        address account = msg.sender;
        risky.safeTransferFrom(account, address(this), riskyIn);

        risky.approve(address(venue), riskyIn);
        settlementOut = venue.sell(riskyIn);
        risky.approve(address(venue), 0);

        require(settlementOut > 0, NothingReceived());
        _requireFairSell(riskyIn, settlementOut);

        settlement.safeTransfer(account, settlementOut);
        emit Unwound(account, riskyIn, settlementOut);
    }

    /// @dev Compare what the venue gave against what the independent price
    /// says the input was worth.
    function _requireFairBuy(uint256 settlementIn, uint256 riskyOut) private view {
        (uint256 price, uint32 scale) = _freshPrice();
        // Expected risky units for this settlement at the reference price.
        uint256 expected = (settlementIn * (10 ** riskyDecimals)) / price;
        _requireWithinTolerance(expected, riskyOut);
        scale; // scale is carried for callers; the ratio above is scale-free
    }

    function _requireFairSell(uint256 riskyIn, uint256 settlementOut) private view {
        (uint256 price,) = _freshPrice();
        uint256 expected = (riskyIn * price) / (10 ** riskyDecimals);
        _requireWithinTolerance(expected, settlementOut);
    }

    /// @dev Rejects a result that falls short of the reference by more than
    /// the tolerance. Receiving MORE than expected is not an error: it
    /// costs the account nothing, and refusing it would block a favourable
    /// fill for no benefit.
    function _requireWithinTolerance(uint256 expected, uint256 actual) private view {
        if (actual >= expected) {
            return;
        }
        uint256 shortfall = expected - actual;
        if (shortfall * 10_000 > expected * maxSlippageBps) {
            revert SlippageExceeded(expected, actual, maxSlippageBps);
        }
    }

    function _freshPrice() private view returns (uint256 price, uint32 scale) {
        uint64 observedAt;
        (price, scale, observedAt) = priceSource.riskyPrice();
        require(price > 0, PriceUnavailable());
        // A stale reference cannot detect a bad fill, so a trade against one
        // is refused rather than waved through.
        require(
            block.timestamp <= observedAt + maxPriceAgeSeconds,
            PriceStale(observedAt, maxPriceAgeSeconds)
        );
    }
}
