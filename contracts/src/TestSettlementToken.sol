// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

/// @title TestSettlementToken
/// @notice A deliberately worthless settlement token for the prototype.
///
/// `docs/10` requires named test assets. The name and symbol here are
/// chosen so no screenshot, explorer listing, or demo frame can be mistaken
/// for issuer-backed USDC: it is not called USDC, it does not claim a peg,
/// and anyone may mint it.
///
/// Open minting is the point. A token anyone can create has no value to
/// misrepresent, which is safer for a public testnet than a supply-limited
/// one that might be treated as if it mattered.
contract TestSettlementToken {
    string public constant name = "Credence Test Settlement (NOT USDC)";
    string public constant symbol = "tCRED";
    uint8 public constant decimals = 6;

    uint256 public totalSupply;
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    error InsufficientBalance(uint256 available, uint256 required);
    error InsufficientAllowance(uint256 available, uint256 required);

    /// @notice Mint to any address. Unrestricted by design.
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
        // An unlimited allowance is not decremented, matching common ERC-20
        // behaviour so integrations behave the same against this token.
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
