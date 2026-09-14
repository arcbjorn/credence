// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {SafeTransfer} from "./SafeTransfer.sol";

/// @dev The slice of the oracle a credit account needs. Narrow on purpose:
/// the account asks whether it may act, and nothing else.
interface IRiskOracle {
    function riskAllowedFor(bytes32 key, bytes32 bindingCommitment, uint64 stateVersion)
        external
        view
        returns (bool);

    /// The highest assessment sequence accepted for a binding. Strictly
    /// increasing, so a value above the quote's own sequence means
    /// something newer has superseded it.
    function lastSequence(bytes32 key) external view returns (uint64);
}

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

    /// @dev Entry points that only ever reduce risk, by selector. These
    /// need no standing permission: invariant 3 already exempts risk
    /// REDUCTION, because a gate that blocks de-risking would trap an
    /// account in exactly the position it needs to exit (`docs/02`).
    /// Registered by the loan manager, never by the agent.
    mapping(address adapter => mapping(bytes4 selector => bool)) public deRiskingAction;

    /// @dev Recovery keeper, set by the loan manager when recovery opens.
    address public recoveryKeeper;

    /// @dev Hash of the binding the live loan was originated against, and
    /// the action its terms permit. Pinned by the loan manager at
    /// origination so `execute` can check authorization without trusting
    /// anything the caller supplies (`docs/05`).
    bytes32 public boundBindingHash;
    bytes32 public boundBindingKey;
    bytes32 public permittedActionHash;
    /// @dev When the origination quote that authorized the first action
    /// expires. The initial atomic action is covered by the consumed quote
    /// (`docs/05`) -- and an expired quote covers nothing, so a deferred
    /// first action needs a current monitoring result like any other.
    uint64 public permittedActionValidUntil;
    /// @dev The assessment sequence the origination quote was signed at.
    /// A newer accepted assessment supersedes that authorization, so the
    /// first action stops being covered by it (`docs/05`).
    uint64 public permittedActionSequence;
    /// @dev True once the origination-authorized action has been taken.
    /// Any further action needs live monitoring permission.
    bool public actionConsumed;

    /// @dev The oracle authorization is checked against. Immutable: a
    /// swappable oracle would make every permission check meaningless.
    address public immutable riskOracle;

    error NotLoanManager();
    error NotOwnerOrAgent();
    error NotKeeper();
    error AdapterNotAllowed(address adapter);
    error WithdrawalBlockedByDebt();
    error RecoveryNotOpen();
    error AdapterCallFailed();
    error ZeroAddress();
    error RiskNotPermitted();
    error ActionNotPermitted(bytes32 expected, bytes32 presented);
    error ExecutionBlockedByRecovery();

    event AdapterSet(address indexed adapter, bool allowed);
    event DeRiskingActionSet(address indexed adapter, bytes4 selector, bool isDeRisking);
    event Executed(address indexed adapter, uint64 stateVersion);
    event Withdrawn(address indexed token, address indexed to, uint256 amount);
    event AdapterApproved(address indexed token, address indexed adapter, uint256 amount);
    event DebtOpened();
    event DebtClosed();
    event RecoveryOpened(address indexed keeper);

    modifier onlyLoanManager() {
        require(msg.sender == loanManager, NotLoanManager());
        _;
    }

    constructor(address loanManager_, address owner_, address agent_, address riskOracle_) {
        require(loanManager_ != address(0), ZeroAddress());
        require(owner_ != address(0), ZeroAddress());
        require(agent_ != address(0), ZeroAddress());
        require(riskOracle_ != address(0), ZeroAddress());
        loanManager = loanManager_;
        owner = owner_;
        agent = agent_;
        riskOracle = riskOracle_;
    }

    // --- Debt state, driven only by the loan manager ---

    /// @notice Open debt and pin what the loan authorizes.
    /// @dev The binding hash and permitted action come from the signed
    /// assessment the manager consumed. Pinning them here is what lets
    /// `execute` verify authorization against something the caller cannot
    /// choose.
    function openDebt(
        bytes32 bindingKey_,
        bytes32 bindingHash,
        bytes32 actionHash,
        uint64 actionValidUntil,
        uint64 actionSequence
    ) external onlyLoanManager {
        debtOutstanding = true;
        boundBindingKey = bindingKey_;
        boundBindingHash = bindingHash;
        permittedActionHash = actionHash;
        permittedActionValidUntil = actionValidUntil;
        permittedActionSequence = actionSequence;
        actionConsumed = false;
        emit DebtOpened();
    }

    function closeDebt() external onlyLoanManager {
        debtOutstanding = false;
        recoveryKeeper = address(0);
        boundBindingKey = bytes32(0);
        boundBindingHash = bytes32(0);
        permittedActionHash = bytes32(0);
        permittedActionValidUntil = 0;
        permittedActionSequence = 0;
        actionConsumed = false;
        emit DebtClosed();
    }

    function openRecovery(address keeper) external onlyLoanManager {
        require(keeper != address(0), ZeroAddress());
        recoveryKeeper = keeper;
        emit RecoveryOpened(keeper);
    }

    /// @notice Declare an adapter entry point as risk-reducing.
    function setDeRiskingAction(address adapter, bytes4 selector, bool isDeRisking)
        external
        onlyLoanManager
    {
        deRiskingAction[adapter][selector] = isDeRisking;
        emit DeRiskingActionSet(adapter, selector, isDeRisking);
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

        // Recovery is the keeper's path. Letting the owner or agent keep
        // trading an account under recovery would let them race the
        // liquidation they are being recovered from.
        require(recoveryKeeper == address(0), ExecutionBlockedByRecovery());

        // While debt is live, every action needs standing permission and
        // must be the action that was assessed. An allowlisted adapter
        // says the route is known; it says nothing about whether acting
        // now is safe, which is the engine's judgment (`docs/02`, `05`).
        //
        // Unfunded accounts are unrestricted: there is no lender exposure
        // to protect, and requiring a quote to move one's own equity would
        // make the account unusable before its first loan.
        // A risk-reducing call is always permitted while debt is live.
        // Invariant 3 constrains actions that INCREASE risk; blocking an
        // exit would leave the account stuck in the position a
        // reassessment is telling it to close.
        bool deRisking = data.length >= 4 && deRiskingAction[adapter][bytes4(data[:4])];

        if (debtOutstanding && !deRisking) {
            bytes32 action = keccak256(abi.encode(adapter, data));
            require(
                action == permittedActionHash,
                ActionNotPermitted(permittedActionHash, action)
            );

            // The origination assessment authorized this exact action, so
            // performing it once needs no separate monitoring result --
            // requiring one would mean no loan could ever deploy the
            // principal it was just granted.
            //
            // But only WHILE THAT QUOTE IS LIVE. Origination and execution
            // are separate transactions, so without the deadline a
            // borrower could wait out the quote, receive an explicit
            // Blocked, and still execute the committed action on authority
            // that had already lapsed.
            // The origination quote covers the first action while it is
            // live AND while nothing newer has been accepted. Every
            // accepted assessment supersedes unused older quotes
            // (`docs/05`), so a Blocked monitoring result arriving before
            // expiry must revoke this coverage -- otherwise a borrower
            // reads the revocation and executes anyway.
            bool superseded = IRiskOracle(riskOracle).lastSequence(boundBindingKey)
                > permittedActionSequence;
            bool coveredByQuote = !actionConsumed
                && block.timestamp < permittedActionValidUntil
                && !superseded;

            if (!coveredByQuote) {
                require(
                    IRiskOracle(riskOracle).riskAllowedFor(
                        boundBindingKey, boundBindingHash, stateVersion
                    ),
                    RiskNotPermitted()
                );
            }
            actionConsumed = true;
        }

        // Invariant: every execution invalidates prior action assumptions.
        //
        // Bumped BEFORE the call, not after. The version is what makes a
        // standing authorization stale, so a compromised adapter that
        // reenters here would otherwise look at the very version its first
        // call was permitted under and act twice on one assessment.
        // Checks-effects-interactions, the ordering `repay` already uses.
        // Reported by review, 14 September.
        unchecked {
            stateVersion++;
        }

        bool ok;
        (ok, result) = adapter.call(data);
        require(ok, AdapterCallFailed());

        emit Executed(adapter, stateVersion);
    }

    /// @notice Grant a registered adapter a bounded allowance to pull one
    /// asset.
    ///
    /// Restricted to whitelisted adapters. Routing approvals through
    /// `execute` would mean registering token contracts as adapters, which
    /// would let any `approve(spender, amount)` through to any spender —
    /// the exact hole the adapter whitelist exists to close.
    ///
    /// The allowance is set to an explicit amount rather than an unlimited
    /// one, so a compromised adapter cannot drain more than the operation
    /// it was granted for.
    /// Setting an allowance to zero is always permitted, including for a
    /// deregistered adapter. Otherwise revoking an adapter would strand a
    /// live allowance that could never be cleared — turning a revocation
    /// into a permanent claim on the account.
    function approveAdapter(address token, address adapter, uint256 amount) external {
        require(msg.sender == owner || msg.sender == agent, NotOwnerOrAgent());
        require(amount == 0 || adapterAllowed[adapter], AdapterNotAllowed(adapter));
        IERC20(token).approve(adapter, amount);
        emit AdapterApproved(token, adapter, amount);
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

        // Before the call, as in `execute`. Recovery reaches the same
        // adapters, so it inherits the same reentrancy question.
        unchecked {
            stateVersion++;
        }

        bool ok;
        (ok, result) = adapter.call(data);
        require(ok, AdapterCallFailed());

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
