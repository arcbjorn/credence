// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IPriceSource} from "./IPriceSource.sol";

/// @title FixedPriceSource
/// @notice A declared price for the prototype's risky asset.
///
/// The prototype has no oracle for a test asset. Publishing a declared
/// value with its observation time is honest; deriving one from the venue
/// would make the slippage check circular — the venue would be validating
/// itself.
contract FixedPriceSource is IPriceSource {
    uint256 public price;
    uint32 public immutable scale;
    uint64 public observedAt;
    address public immutable operator;

    error NotOperator();

    constructor(uint256 price_, uint32 scale_, address operator_) {
        price = price_;
        scale = scale_;
        observedAt = uint64(block.timestamp);
        operator = operator_;
    }

    function setPrice(uint256 price_) external {
        require(msg.sender == operator, NotOperator());
        price = price_;
        observedAt = uint64(block.timestamp);
    }

    /// @notice Refresh the observation without changing the price, so a
    /// scenario can hold a price steady across advancing chain time.
    function touch() external {
        require(msg.sender == operator, NotOperator());
        observedAt = uint64(block.timestamp);
    }

    function riskyPrice() external view returns (uint256, uint32, uint64) {
        return (price, scale, observedAt);
    }
}
