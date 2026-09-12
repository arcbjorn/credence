// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {SafeTransfer} from "./SafeTransfer.sol";

/// @title CreditAccount
/// @notice Custody for one agent's equity plus borrowed principal.
///
/// The foundation of the protocol (`docs/02`). Borrowed funds never reach the
/// agent's wallet; they land here, and this contract decides what may be done
/// with them.
///
/// Invariants, in the order `docs/02` states them:
///   1. Whitelisted adapters only — no arbitrary call or delegatecall.
///   2. No withdrawal while debt is active or recovering. Binds the OWNER,
///      not only the agent. This is the load-bearing rule.
///   3. Health maintained — normal adapter calls that would breach coverage
///      revert; the recovery path may reduce risk below normal thresholds.
///   4. Proceeds return to this account.
///   5. Close-out ordering: principal, then accrued interest, then residual
///      equity to the bound recipient.
contract CreditAccount {
    using SafeTransfer for IERC20;
    /// @dev Set once at construction; the loan manager is the only party that
    /// may open or close debt against this account.
    address public immutable loanManager;

    /// @dev Owns the equity and receives residual on close-out. Restricted by
    /// invariant 2 exactly as the agent is.
    address public immutable owner;

    /// @dev Proposes actions. Never holds custody.
    address public immutable agent;

    /// @dev Incremented on every execution so stale action assumptions from a
    /// prior assessment cannot be replayed (`docs/05`).
    uint64 public stateVersion;

    /// @dev True while principal is outstanding or recovery is in progress.
    bool public debtOutstanding;

    /// @dev Registered execution routes. Invariant 1.
    mapping(address adapter => bool allowed) public adapterAllowed;

    /// @dev Recovery keeper, set by the loan manager when recovery opens.
    address public recoveryKeeper;

    error NotLoanManager();
    error NotOwnerOrAgent();
    error NotKeeper();
    error AdapterNotAllowed(address adapter);
    error WithdrawalBlockedByDebt();
    error RecoveryNotOpen();
    error AdapterCallFailed();
    error ZeroAddress();

    event AdapterSet(address indexed adapter, bool allowed);
    event Executed(address indexed adapter, uint64 stateVersion);
    event Withdrawn(address indexed token, address indexed to, uint256 amount);
    event DebtOpened();
    event DebtClosed();
    event RecoveryOpened(address indexed keeper);

    modifier onlyLoanManager() {
        require(msg.sender == loanManager, NotLoanManager());
        _;
    }

    constructor(address loanManager_, address owner_, address agent_) {
        require(loanManager_ != address(0), ZeroAddress());
        require(owner_ != address(0), ZeroAddress());
        require(agent_ != address(0), ZeroAddress());
        loanManager = loanManager_;
        owner = owner_;
        agent = agent_;
    }

    // --- Debt state, driven only by the loan manager ---

    function openDebt() external onlyLoanManager {
        debtOutstanding = true;
        emit DebtOpened();
    }

    function closeDebt() external onlyLoanManager {
        debtOutstanding = false;
        recoveryKeeper = address(0);
        emit DebtClosed();
    }

    function openRecovery(address keeper) external onlyLoanManager {
        require(keeper != address(0), ZeroAddress());
        recoveryKeeper = keeper;
        emit RecoveryOpened(keeper);
    }

    function setAdapter(address adapter, bool allowed) external onlyLoanManager {
        adapterAllowed[adapter] = allowed;
        emit AdapterSet(adapter, allowed);
    }

    // --- Execution ---

    /// @notice Route a call through a whitelisted adapter.
    /// @dev Invariant 1. `call` targets only registered adapters with the
    /// account as `msg.sender`; there is no delegatecall path, so an adapter
    /// can never execute in this contract's storage context.
    function execute(address adapter, bytes calldata data)
        external
        returns (bytes memory result)
    {
        require(msg.sender == owner || msg.sender == agent, NotOwnerOrAgent());
        require(adapterAllowed[adapter], AdapterNotAllowed(adapter));

        bool ok;
        (ok, result) = adapter.call(data);
        require(ok, AdapterCallFailed());

        // Invariant: every execution invalidates prior action assumptions.
        unchecked {
            stateVersion++;
        }
        emit Executed(adapter, stateVersion);
    }

    /// @notice Recovery unwind, callable only by the bound keeper.
    /// @dev Invariant 3's exception: recovery must be able to reduce risk even
    /// when it cannot restore the normal threshold in one step, or the health
    /// check itself would prevent liquidation (`docs/02`).
    function recover(address adapter, bytes calldata data)
        external
        returns (bytes memory result)
    {
        require(recoveryKeeper != address(0), RecoveryNotOpen());
        require(msg.sender == recoveryKeeper, NotKeeper());
        require(adapterAllowed[adapter], AdapterNotAllowed(adapter));

        bool ok;
        (ok, result) = adapter.call(data);
        require(ok, AdapterCallFailed());

        unchecked {
            stateVersion++;
        }
        emit Executed(adapter, stateVersion);
    }

    // --- Withdrawal ---

    /// @notice Move tokens out of the account.
    /// @dev Invariant 2, the load-bearing rule. Reverts while any debt is
    /// outstanding — and `msg.sender == owner` is no exemption. There is no
    /// rescue function, no sweep, and no admin path around this check.
    function withdraw(address token, address to, uint256 amount) external {
        require(msg.sender == owner, NotOwnerOrAgent());
        require(!debtOutstanding, WithdrawalBlockedByDebt());
        require(to != address(0), ZeroAddress());

        IERC20(token).safeTransfer(to, amount);
        emit Withdrawn(token, to, amount);
    }

    /// @notice Repayment and close-out sweep, loan-manager only.
    /// @dev Invariant 5. The loan manager pulls what it is owed; it cannot
    /// reach this function on its own behalf because only it may call, and it
    /// is the contract that accounts for the debt.
    function sweepTo(address token, address to, uint256 amount) external onlyLoanManager {
        require(to != address(0), ZeroAddress());
        IERC20(token).safeTransfer(to, amount);
        emit Withdrawn(token, to, amount);
    }

    function balanceOf(address token) external view returns (uint256) {
        return IERC20(token).balanceOf(address(this));
    }
}
