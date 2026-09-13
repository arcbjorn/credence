// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @title TestRiskyAsset
/// @notice The one supported risky asset for the prototype.
///
/// Named for what it is, like the settlement token: `docs/10` requires
/// named test assets, and a demo screenshot must never be mistakable for a
/// real one. Anyone may mint, so it has no value to misrepresent.
///
/// Eighteen decimals, unlike the six-decimal settlement token. That
/// asymmetry is deliberate — it is the common real-world case, and a
/// prototype where every asset shares one scale would hide unit errors
/// until they mattered.
contract TestRiskyAsset {
    string public constant name = "Credence Test Risky Asset (NOT ETH)";
    string public constant symbol = "tRISK";
    uint8 public constant decimals = 18;

    uint256 public totalSupply;
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    error InsufficientBalance(uint256 available, uint256 required);
    error InsufficientAllowance(uint256 available, uint256 required);

    function mint(address to, uint256 amount) external {
        balanceOf[to] += amount;
        totalSupply += amount;
        emit Transfer(address(0), to, amount);
    }

    function transfer(address to, uint256 value) external returns (bool) {
        _transfer(msg.sender, to, value);
        return true;
    }

    function transferFrom(address from, address to, uint256 value) external returns (bool) {
        uint256 allowed = allowance[from][msg.sender];
        if (allowed != type(uint256).max) {
            require(allowed >= value, InsufficientAllowance(allowed, value));
            allowance[from][msg.sender] = allowed - value;
        }
        _transfer(from, to, value);
        return true;
    }

    function approve(address spender, uint256 value) external returns (bool) {
        allowance[msg.sender][spender] = value;
        emit Approval(msg.sender, spender, value);
        return true;
    }

    function _transfer(address from, address to, uint256 value) private {
        uint256 balance = balanceOf[from];
        require(balance >= value, InsufficientBalance(balance, value));
        unchecked {
            balanceOf[from] = balance - value;
        }
        balanceOf[to] += value;
        emit Transfer(from, to, value);
    }
}
