// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {SafeTransfer} from "./SafeTransfer.sol";

/// @title NoteSeries
/// @notice A single loan funded by transferable notes rather than a pool.
///
/// `docs/08`: "A separate NoteSeries funding route for one loan in an
/// isolated deployment. Fixed supply issued at origination; notes are a
/// pro-rata claim on that loan's actual net collections."
///
/// This exposes the same funding-source interface `LoanManager` already
/// calls on `CreditVault` -- `lend`, `recordPrincipalRepaid`,
/// `recordInterestCollected`, `recordLoss`, `markReceivableUnresolved`,
/// `clearReceivable`, `releaseReservation` -- so the manager binds to one
/// or the other without knowing which. The manager is unchanged.
///
/// What differs is who is owed and how they are paid:
///
///   CreditVault   pooled capital, shares fixed at deposit, one settlement
///                 for every loan in the epoch
///   NoteSeries    ONE loan, fixed supply issued at funding, notes
///                 transferable while the loan runs, redeemed against that
///                 loan's own collections
///
///   Funding ──► Active ──► Redeemable
///
/// **Never tokenize a receivable belonging to the pooled vault.** Each
/// `LoanManager` binds to exactly one funding source, so a loan funded here
/// was never in the pool and cannot be claimed twice. That is structural:
/// the manager holds a single `vault` address.
///
/// Three rules from `docs/08` shape the redemption arithmetic:
///
///   1. Notes are a claim on ACTUAL net collections, not on face value.
///      Default redeems below principal, and that is the instrument
///      working rather than failing.
///   2. Early repayment pays accrued interest only. The note does not
///      promise unearned full-term interest, so `reservedInterest` is
///      tracked for the manager's cap arithmetic and never distributed.
///   3. Pending recovery means pending redemption. A loan still being
///      collected is not redeemable, because paying the first holder out
///      of an incomplete pot would pay them from a later holder's share.
contract NoteSeries {
    using SafeTransfer for IERC20;

    enum Phase {
        Funding,
        Active,
        Redeemable
    }

    // --- Series terms, fixed before funding (`docs/08`) ---

    IERC20 public immutable asset;
    address public immutable operator;
    /// @notice The borrower this series exists to fund. One loan, one series.
    address public immutable borrower;
    /// @notice Face value of the whole series, and the principal it lends.
    uint256 public immutable targetPrincipal;
    /// @notice Contractual term in seconds, recorded for holders to read.
    uint64 public immutable termSeconds;
    /// @notice Contractual rate, recorded for holders to read. The series
    ///         pays what is COLLECTED; this is what was promised, not a
    ///         guarantee of what arrives.
    uint32 public immutable aprBps;

    address public loanManager;

    Phase public phase;

    // --- Ledgers, separate for the same reason CreditVault separates its ---

    /// @notice Settlement tokens subscribed but not yet lent.
    uint256 public idleCash;
    /// @notice Principal currently out with the borrower.
    uint256 public outstandingPrincipal;
    /// @notice Interest actually received. Not accrued, not promised.
    uint256 public collectedInterest;
    /// @notice Principal written off and never coming back.
    uint256 public realizedLoss;
    /// @notice Interest reserved against the manager's cap, never paid out.
    uint256 public reservedObligation;
    /// @notice Amounts still being collected. Non-zero blocks redemption.
    uint256 public unresolvedReceivables;

    // --- The notes themselves: a minimal transferable ERC20 ---

    string public constant name = "Credence Note Series";
    string public constant symbol = "CNOTE";
    uint8 public immutable decimals;

    uint256 public totalSupply;
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    /// @notice Notes already redeemed, so the last holder absorbs the dust.
    uint256 public redeemedNotes;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
    event Subscribed(address indexed holder, uint256 amount);
    event Activated(uint256 supply);
    event Lent(address indexed account, uint256 principal);
    event PrincipalRepaid(address indexed account, uint256 principal);
    event InterestCollected(address indexed account, uint256 interest);
    event LossRealized(address indexed account, uint256 principal);
    event Redeemable(uint256 distributable);
    event Redeemed(address indexed holder, uint256 notes, uint256 amount);
    event PhaseChanged(Phase from, Phase to);

    error NotOperator();
    error NotLoanManager();
    error WrongPhase(Phase expected, Phase actual);
    error ZeroAmount();
    error ZeroAddress();
    error ExceedsTarget();
    error UnderSubscribed(uint256 raised, uint256 target);
    error NotThisBorrower();
    error RecoveryPending(uint256 amount);
    error InsufficientIdleCash();
    error NothingToRedeem();
    error InsufficientBalance();
    error InsufficientAllowance();
    error ManagerAlreadySet();

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

    constructor(
        address asset_,
        address operator_,
        address borrower_,
        uint256 targetPrincipal_,
        uint64 termSeconds_,
        uint32 aprBps_,
        uint8 decimals_
    ) {
        require(asset_ != address(0), ZeroAddress());
        require(operator_ != address(0), ZeroAddress());
        require(borrower_ != address(0), ZeroAddress());
        require(targetPrincipal_ > 0, ZeroAmount());

        asset = IERC20(asset_);
        operator = operator_;
        borrower = borrower_;
        targetPrincipal = targetPrincipal_;
        termSeconds = termSeconds_;
        aprBps = aprBps_;
        // Notes are denominated in the settlement asset, so one note is one
        // base unit of face value and redemption arithmetic needs no scaling.
        decimals = decimals_;
        phase = Phase.Funding;
    }

    /// @notice Bind the manager allowed to draw on this series.
    /// @dev Set once. A series whose manager could be repointed mid-loan
    ///      would let the operator move an obligation holders already own.
    function setLoanManager(address loanManager_) external onlyOperator {
        require(loanManager == address(0), ManagerAlreadySet());
        require(loanManager_ != address(0), ZeroAddress());
        loanManager = loanManager_;
    }

    // --- Funding ---

    /// @notice Subscribe settlement tokens for notes, one for one.
    /// @dev Fixed supply at origination (`docs/08`), so subscription closes
    ///      when the target is reached and cannot be exceeded.
    function subscribe(uint256 amount) external inPhase(Phase.Funding) {
        require(amount > 0, ZeroAmount());
        require(idleCash + amount <= targetPrincipal, ExceedsTarget());

        idleCash += amount;
        totalSupply += amount;
        balanceOf[msg.sender] += amount;

        asset.safeTransferFrom(msg.sender, address(this), amount);
        emit Subscribed(msg.sender, amount);
        emit Transfer(address(0), msg.sender, amount);
    }

    /// @notice Close subscription and allow the loan to be drawn.
    /// @dev Requires the FULL target. A partially funded series would issue
    ///      notes against a loan the borrower cannot actually draw, and the
    ///      shortfall would surface as a phantom loss at redemption.
    function activate() external onlyOperator inPhase(Phase.Funding) {
        require(idleCash == targetPrincipal, UnderSubscribed(idleCash, targetPrincipal));
        _setPhase(Phase.Active);
        emit Activated(totalSupply);
    }

    // --- The funding-source interface `LoanManager` calls ---

    /// @notice Lend the series principal to the account it was raised for.
    /// @dev Only to `borrower`. A pooled vault lends to whichever account
    ///      the manager names; a series exists for ONE loan, and lending
    ///      elsewhere would fund a loan its holders never bought.
    function lend(address account, uint256 principal, uint256 reservedInterest)
        external
        onlyLoanManager
        inPhase(Phase.Active)
    {
        require(account == borrower, NotThisBorrower());
        require(principal > 0, ZeroAmount());
        require(principal <= idleCash, InsufficientIdleCash());

        idleCash -= principal;
        outstandingPrincipal += principal;
        reservedObligation += reservedInterest;

        asset.safeTransfer(account, principal);
        emit Lent(account, principal);
    }

    /// @notice Record returned principal. Tokens must already be held here.
    function recordPrincipalRepaid(address account, uint256 principal) external onlyLoanManager {
        outstandingPrincipal -= principal;
        idleCash += principal;
        emit PrincipalRepaid(account, principal);
    }

    /// @notice Record interest actually received.
    /// @dev `docs/08`: early repayment pays accrued interest only. What
    ///      arrives here is what was collected; the note never promised the
    ///      unearned remainder of a full term.
    function recordInterestCollected(address account, uint256 interest) external onlyLoanManager {
        collectedInterest += interest;
        idleCash += interest;
        emit InterestCollected(account, interest);
    }

    /// @notice Write off principal that will not return.
    /// @dev Reduces exposure WITHOUT increasing idle cash, so the loss lands
    ///      on holders at redemption. `docs/08`: "Default can redeem below
    ///      principal."
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

    function releaseReservation(uint256 amount) external onlyLoanManager {
        reservedObligation -= amount;
    }

    // --- Redemption ---

    /// @notice Open redemption once the loan has finished resolving.
    /// @dev `docs/08`: "pending recovery means pending redemption." Opening
    ///      early would pay the first holder out of an incomplete pot, which
    ///      is the later holders' money.
    function openRedemption() external onlyOperator inPhase(Phase.Active) {
        require(unresolvedReceivables == 0, RecoveryPending(unresolvedReceivables));
        require(outstandingPrincipal == 0, RecoveryPending(outstandingPrincipal));

        _setPhase(Phase.Redeemable);
        emit Redeemable(idleCash);
    }

    /// @notice Burn notes for a pro-rata share of actual collections.
    /// @dev Remaining-cash over remaining-notes, matching `CreditVault.claim`:
    ///      integer truncation accumulates to the final redeemer rather than
    ///      stranding dust in the contract (`docs/05`).
    ///
    ///      The amount is deliberately NOT face value. A series that
    ///      collected less than it lent pays less than face, and that is the
    ///      instrument behaving correctly.
    function redeem() external inPhase(Phase.Redeemable) returns (uint256 amount) {
        uint256 notes = balanceOf[msg.sender];
        require(notes > 0, NothingToRedeem());

        uint256 remaining = totalSupply - redeemedNotes;
        amount = (idleCash * notes) / remaining;

        balanceOf[msg.sender] = 0;
        redeemedNotes += notes;
        idleCash -= amount;

        asset.safeTransfer(msg.sender, amount);
        emit Redeemed(msg.sender, notes, amount);
        emit Transfer(msg.sender, address(0), notes);
    }

    // --- ERC20: what makes the claim tradable ---

    function transfer(address to, uint256 value) external returns (bool) {
        _transfer(msg.sender, to, value);
        return true;
    }

    function transferFrom(address from, address to, uint256 value) external returns (bool) {
        uint256 allowed = allowance[from][msg.sender];
        if (allowed != type(uint256).max) {
            require(allowed >= value, InsufficientAllowance());
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

    /// @dev Transfer is permitted in every phase, including Redeemable.
    ///      A holder who wants out after the outcome is known should be able
    ///      to sell rather than being forced to redeem -- that secondary
    ///      price is the market signal `docs/08` keeps INFORMATIONAL.
    function _transfer(address from, address to, uint256 value) private {
        require(to != address(0), ZeroAddress());
        require(balanceOf[from] >= value, InsufficientBalance());

        balanceOf[from] -= value;
        balanceOf[to] += value;
        emit Transfer(from, to, value);
    }

    // --- Views ---

    /// @notice What one note would redeem for right now, in base units.
    /// @dev Zero before redemption opens: the pot is not final, and quoting
    ///      a number from an incomplete collection would be a marked
    ///      estimate dressed as a settlement.
    function redemptionPerNote() external view returns (uint256) {
        if (phase != Phase.Redeemable) return 0;
        uint256 remaining = totalSupply - redeemedNotes;
        if (remaining == 0) return 0;
        return (idleCash * 1e18) / remaining;
    }

    function _setPhase(Phase next) private {
        emit PhaseChanged(phase, next);
        phase = next;
    }
}
