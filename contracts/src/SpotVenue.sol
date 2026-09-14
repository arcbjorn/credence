// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {SafeTransfer} from "./SafeTransfer.sol";

/// @title SpotVenue
/// @notice A minimal spot market for the prototype's one asset pair.
///
/// Deliberately able to quote a bad price. A venue that could only trade
/// fairly would make the adapter's slippage guard untestable, and `docs/05`
/// requires proving that a whitelisted venue is not sufficient on its own.
contract SpotVenue {
    using SafeTransfer for IERC20;

    IERC20 public immutable settlement;
    IERC20 public immutable risky;

    /// @dev Settlement base units per whole risky unit, at `priceScale`.
    uint256 public price;
    uint32 public immutable priceScale;
    uint32 public immutable riskyDecimals;

    address public immutable operator;

    error NotOperator();

    constructor(
        IERC20 settlement_,
        IERC20 risky_,
        uint256 price_,
        uint32 priceScale_,
        uint32 riskyDecimals_,
        address operator_
    ) {
        settlement = settlement_;
        risky = risky_;
        price = price_;
        priceScale = priceScale_;
        riskyDecimals = riskyDecimals_;
        operator = operator_;
    }

    /// @notice Move the quoted price. Used to stage an adverse unwind.
    function setPrice(uint256 price_) external {
        require(msg.sender == operator, NotOperator());
        price = price_;
    }

    /// @notice Buy risky with settlement at the current quote.
    function buy(uint256 settlementIn) external returns (uint256 riskyOut) {
        settlement.safeTransferFrom(msg.sender, address(this), settlementIn);
        riskyOut = quoteBuy(settlementIn);
        risky.safeTransfer(msg.sender, riskyOut);
    }

    /// @notice Sell risky for settlement at the current quote.
    function sell(uint256 riskyIn) external returns (uint256 settlementOut) {
        risky.safeTransferFrom(msg.sender, address(this), riskyIn);
        settlementOut = quoteSell(riskyIn);
        settlement.safeTransfer(msg.sender, settlementOut);
    }

    function quoteBuy(uint256 settlementIn) public view returns (uint256) {
        // settlementIn / price, expressed in risky decimals.
        return
            (settlementIn * (10 ** priceScale) * (10 ** riskyDecimals))
                / (price * (10 ** priceScale));
    }

    function quoteSell(uint256 riskyIn) public view returns (uint256) {
        return (riskyIn * price) / (10 ** riskyDecimals);
    }
}
