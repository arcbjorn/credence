// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";

/// @notice Transfer helpers that treat a `false` return as failure.
/// @dev Some tokens return false rather than reverting. Ignoring the return
/// value would let a repayment or sweep silently move nothing while the
/// caller records success — an accounting divergence between token balances
/// and protocol state.
library SafeTransfer {
    error TransferFailed();

    function safeTransfer(IERC20 token, address to, uint256 value) internal {
        (bool ok, bytes memory data) =
            address(token).call(abi.encodeCall(IERC20.transfer, (to, value)));
        require(ok && (data.length == 0 || abi.decode(data, (bool))), TransferFailed());
    }

    function safeTransferFrom(IERC20 token, address from, address to, uint256 value) internal {
        (bool ok, bytes memory data) =
            address(token).call(abi.encodeCall(IERC20.transferFrom, (from, to, value)));
        require(ok && (data.length == 0 || abi.decode(data, (bool))), TransferFailed());
    }
}
