// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {SafeTransfer} from "./SafeTransfer.sol";

/// @title CreditVault
/// @notice Fixed-epoch lender capital with explicit, separated ledgers.
///
/// A fixed epoch avoids building instantaneous redemption or fair-value share
/// pricing while loans are outstanding (`docs/05`). Lenders accept a defined
/// lockup; in exchange, settlement distributes ACTUAL final cash rather than
/// a marked estimate.
///
///   Funding ──► Active ──► Resolving ──► Settled
///
/// The four ledgers are kept separate on purpose. Conflating them is how a
/// vault ends up paying out interest it never collected.
contract CreditVault {
    using SafeTransfer for IERC20;

    enum Phase {
        Funding,
        Active,
        Resolving,
        Settled
    }

    IERC20 public immutable asset;
    address public immutable operator;
    address public loanManager;

    Phase public phase;

    /// @dev Cash physically held and free to lend or distribute.
    uint256 public idleCash;
    /// @dev Principal currently out with borrowers.
    uint256 public outstandingPrincipal;
    /// @dev Principal that will never return. Recorded at default.
    uint256 public realizedLoss;
    /// @dev Interest actually received. Never includes accrued-but-unpaid —
    /// `docs/05`: uncollected interest is never withdrawable cash.
    uint256 public collectedInterest;

    /// @dev Ceiling on total principal outstanding at once.
    uint256 public globalCap;

    /// @dev Unresolved receivables. While non-zero the epoch cannot settle,
    /// because a material receivable is still outstanding (`docs/05`).
    uint256 public unresolvedReceivables;

    mapping(address lender => uint256 shares) public shares;
    uint256 public totalShares;

    /// @dev Cash frozen at settlement, so later dust math stays stable.
    uint256 public settlementCash;
    uint256 public claimedShares;

    error NotOperator();
    error NotLoanManager();
    error WrongPhase(Phase expected, Phase actual);
    error ExceedsGlobalCap();
    error InsufficientIdleCash();
    error NothingToClaim();
    error ReceivablesUnresolved(uint256 amount);
    error ZeroAmount();

    event Deposited(address indexed lender, uint256 amount, uint256 sharesIssued);
    event PhaseChanged(Phase indexed from, Phase indexed to);
    event Lent(address indexed account, uint256 principal);
    event PrincipalRepaid(address indexed account, uint256 principal);
    event InterestCollected(address indexed account, uint256 interest);
    event LossRealized(address indexed account, uint256 principal);
    event Claimed(address indexed lender, uint256 sharesBurned, uint256 amount);

    modifier onlyOperator() {
        require(msg.sender == operator, NotOperator());
        _;
    }

    modifier onlyLoanManager() {
        require(msg.sender == loanManager, NotLoanManager());
        _;
    }

    modifier inPhase(Phase expected) {
        require(phase == expected, WrongPhase(expected, phase));
        _;
    }

    constructor(IERC20 asset_, address operator_, uint256 globalCap_) {
        asset = asset_;
        operator = operator_;
        globalCap = globalCap_;
    }

    function setLoanManager(address loanManager_) external onlyOperator {
        loanManager = loanManager_;
    }

    // --- Funding phase ---

    /// @notice Deposit at par. Shares equal base units contributed.
    /// @dev Par issuance is only sound because deposits close before lending
    /// begins; no lender can enter after risk is taken.
    function deposit(uint256 amount) external inPhase(Phase.Funding) {
        require(amount > 0, ZeroAmount());
        asset.safeTransferFrom(msg.sender, address(this), amount);
        idleCash += amount;
        shares[msg.sender] += amount;
        totalShares += amount;
        emit Deposited(msg.sender, amount, amount);
    }

    function activate() external onlyOperator inPhase(Phase.Funding) {
        _setPhase(Phase.Active);
    }

    /// @notice Stop new originations ahead of settlement.
    function beginResolving() external onlyOperator inPhase(Phase.Active) {
        _setPhase(Phase.Resolving);
    }

    /// @notice Freeze final cash for pro-rata distribution.
    /// @dev Reverts while any receivable is unresolved. `docs/05`: the epoch
    /// cannot claim full settlement while a material receivable remains.
    function settle() external onlyOperator inPhase(Phase.Resolving) {
        require(unresolvedReceivables == 0, ReceivablesUnresolved(unresolvedReceivables));
        require(outstandingPrincipal == 0, ReceivablesUnresolved(outstandingPrincipal));
        settlementCash = idleCash;
        _setPhase(Phase.Settled);
    }

    // --- Lending, driven by the loan manager ---

    function lend(address account, uint256 principal)
        external
        onlyLoanManager
        inPhase(Phase.Active)
    {
        require(principal > 0, ZeroAmount());
        require(principal <= idleCash, InsufficientIdleCash());
        require(outstandingPrincipal + principal <= globalCap, ExceedsGlobalCap());

        idleCash -= principal;
        outstandingPrincipal += principal;
        asset.safeTransfer(account, principal);
        emit Lent(account, principal);
    }

    /// @notice Record returned principal. Tokens must already be held here.
    /// @dev Split from interest so the ledgers never blur: principal reduces
    /// exposure, interest is income. Callers transfer first, then record.
    function recordPrincipalRepaid(address account, uint256 principal)
        external
        onlyLoanManager
    {
        outstandingPrincipal -= principal;
        idleCash += principal;
        emit PrincipalRepaid(account, principal);
    }

    function recordInterestCollected(address account, uint256 interest)
        external
        onlyLoanManager
    {
        collectedInterest += interest;
        idleCash += interest;
        emit InterestCollected(account, interest);
    }

    /// @notice Write off principal that will not return.
    /// @dev Reduces outstanding without increasing idle cash — the loss is
    /// borne by lenders at settlement, which is exactly the visible
    /// consequence `docs/09` beat 7 demonstrates.
    function recordLoss(address account, uint256 principal) external onlyLoanManager {
        outstandingPrincipal -= principal;
        realizedLoss += principal;
        emit LossRealized(account, principal);
    }

    function markReceivableUnresolved(uint256 amount) external onlyLoanManager {
        unresolvedReceivables += amount;
    }

    function clearReceivable(uint256 amount) external onlyLoanManager {
        unresolvedReceivables -= amount;
    }

    // --- Settlement ---

    /// @notice Burn shares for a pro-rata portion of actual final cash.
    /// @dev Uses remaining-cash over remaining-shares, so integer truncation
    /// accumulates to the final claimant rather than stranding dust in the
    /// contract (`docs/05`).
    function claim() external inPhase(Phase.Settled) returns (uint256 amount) {
        uint256 userShares = shares[msg.sender];
        require(userShares > 0, NothingToClaim());

        uint256 remainingShares = totalShares - claimedShares;
        amount = (idleCash * userShares) / remainingShares;

        shares[msg.sender] = 0;
        claimedShares += userShares;
        idleCash -= amount;

        asset.safeTransfer(msg.sender, amount);
        emit Claimed(msg.sender, userShares, amount);
    }

    function _setPhase(Phase next) private {
        emit PhaseChanged(phase, next);
        phase = next;
    }
}
