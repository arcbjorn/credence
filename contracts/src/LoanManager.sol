// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {SafeTransfer} from "./SafeTransfer.sol";
import {CreditAccount} from "./CreditAccount.sol";
import {CreditVault} from "./CreditVault.sol";
import {RiskOracle} from "./RiskOracle.sol";
import {Binding, Terms} from "./Assessment.sol";

/// @title LoanManager
/// @notice Origination, interest, repayment, recovery, and loss allocation.
///
/// State machine (`docs/05`):
///
///   NoLoan ── assessment + equity + funding ──► Active
///   Active ── full repayment ─────────────────► Repaid
///   Active ── health breach or maturity ──────► RecoveryPending
///   RecoveryPending ── debt collected ────────► Recovered
///   RecoveryPending ── assets exhausted ──────► Defaulted
///
/// Repaid, Recovered, and Defaulted are terminal. A default leaves the loss
/// permanently attributable to the original binding, and that binding takes
/// no new loans.
contract LoanManager {
    using SafeTransfer for IERC20;

    enum Status {
        NoLoan,
        Active,
        Repaid,
        RecoveryPending,
        Recovered,
        Defaulted
    }

    struct Loan {
        address account;
        uint256 principal;
        uint256 principalOutstanding;
        uint256 interestCollected;
        uint32 aprBps;
        uint64 originatedAt;
        uint64 termSeconds;
        uint64 recoveryStartedAt;
        uint256 interestSnapshot;
        Status status;
    }

    uint256 private constant BPS_DENOM = 10_000;
    uint256 private constant SECONDS_PER_YEAR = 365 days;

    IERC20 public immutable asset;
    CreditVault public immutable vault;
    RiskOracle public immutable oracle;
    address public immutable operator;

    uint256 public nextLoanId = 1;
    mapping(uint256 loanId => Loan) public loans;
    /// @dev One active loan per account (`docs/05`).
    mapping(address account => uint256 loanId) public activeLoan;
    /// @dev Unresolved default flag; blocks new credit for that binding.
    mapping(address account => bool) public defaulted;

    bool public paused;

    error NotOperator();
    error AccountHasActiveLoan();
    error AccountInDefault();
    error WrongStatus(Status expected, Status actual);
    error NotYetDefaultable();
    error Paused();
    error ZeroAmount();
    error NotKeeper();
    error StaleAccountState(uint64 presented, uint64 actual);
    error InsufficientEquity(uint256 required, uint256 held);

    event Originated(uint256 indexed loanId, address indexed account, uint256 principal);
    event Repaid(uint256 indexed loanId, uint256 principal, uint256 interest);
    event RecoveryOpened(uint256 indexed loanId, uint64 at);
    event Collected(uint256 indexed loanId, uint256 principal, uint256 interest);
    event Recovered(uint256 indexed loanId);
    event Defaulted(uint256 indexed loanId, uint256 principalLoss, uint256 unpaidInterest);

    modifier onlyOperator() {
        require(msg.sender == operator, NotOperator());
        _;
    }

    constructor(IERC20 asset_, CreditVault vault_, RiskOracle oracle_, address operator_) {
        asset = asset_;
        vault = vault_;
        oracle = oracle_;
        operator = operator_;
    }

    /// @notice Register an execution route on an account.
    /// @dev The account only accepts adapter changes from its loan manager,
    /// so this is the operator's path to whitelisting a strategy route.
    function setAccountAdapter(CreditAccount account, address adapter, bool allowed)
        external
        onlyOperator
    {
        account.setAdapter(adapter, allowed);
    }

    /// @notice Halt new originations.
    /// @dev `docs/05`: a pause never blocks repayment or permitted recovery.
    /// Only `originate` consults this flag.
    function setPaused(bool value) external onlyOperator {
        paused = value;
    }

    // --- Origination ---

    /// @notice Originate against a standing, unconsumed quote.
    /// @dev Step 6-8 of `docs/05`'s origination sequence. The quote is
    /// consumed and the debt created BEFORE any untrusted external
    /// interaction, so a reentrant adapter cannot observe a funded account
    /// with no recorded obligation.
    ///
    /// The oracle enforces authorization (route, sequence, expiry, class,
    /// exact amount). This function enforces live contract state, because a
    /// correctly signed assessment is not permission to ignore a vault cap
    /// or an open loan.
    function originate(Binding calldata binding, uint256 principal)
        external
        returns (uint256 loanId)
    {
        require(!paused, Paused());
        require(principal > 0, ZeroAmount());

        address acct = binding.creditAccount;
        CreditAccount account = CreditAccount(acct);

        require(activeLoan[acct] == 0, AccountHasActiveLoan());
        require(!defaulted[acct], AccountInDefault());
        // The binding must describe the account as it stands now. A stale
        // state version means the assessed portfolio no longer exists.
        require(
            binding.accountStateVersion == account.stateVersion(),
            StaleAccountState(binding.accountStateVersion, account.stateVersion())
        );

        // Consumes once, and only for this exact amount.
        Terms memory terms = oracle.consumeQuote(binding, principal);

        // `docs/02`: an agent with no equity receives no credit in v0.
        require(
            account.balanceOf(address(asset)) >= terms.requiredEquity,
            InsufficientEquity(terms.requiredEquity, account.balanceOf(address(asset)))
        );

        loanId = nextLoanId++;
        loans[loanId] = Loan({
            account: acct,
            principal: principal,
            principalOutstanding: principal,
            interestCollected: 0,
            aprBps: terms.aprBps,
            originatedAt: uint64(block.timestamp),
            termSeconds: terms.termSeconds,
            recoveryStartedAt: 0,
            interestSnapshot: 0,
            status: Status.Active
        });
        activeLoan[acct] = loanId;

        account.openDebt();
        vault.lend(acct, principal);

        emit Originated(loanId, acct, principal);
    }

    // --- Interest ---

    /// @notice Interest accrued to now, or to recovery entry if it has begun.
    /// @dev Mirrors `risk-core::interest::accrued` exactly: 365-day year,
    /// elapsed clamped to term, obligations round up. Cross-language
    /// reconciliation is a release requirement (`docs/11`).
    function accruedInterest(uint256 loanId) public view returns (uint256) {
        Loan storage loan = loans[loanId];
        if (loan.recoveryStartedAt != 0) {
            return loan.interestSnapshot;
        }
        return _accruedAt(loan, uint64(block.timestamp));
    }

    function _accruedAt(Loan storage loan, uint64 at) private view returns (uint256) {
        uint256 elapsed = at - loan.originatedAt;
        if (elapsed > loan.termSeconds) {
            elapsed = loan.termSeconds;
        }
        uint256 numer = loan.principal * loan.aprBps * elapsed;
        uint256 denom = BPS_DENOM * SECONDS_PER_YEAR;
        // Obligations round up.
        return numer == 0 ? 0 : (numer - 1) / denom + 1;
    }

    function debtDue(uint256 loanId) public view returns (uint256) {
        Loan storage loan = loans[loanId];
        // A resolved loan owes nothing. Without this a repaid loan keeps
        // accruing in views, reporting debt that can never be paid because
        // repay() refuses a terminal status.
        if (
            loan.status == Status.Repaid || loan.status == Status.Recovered
                || loan.status == Status.Defaulted
        ) {
            return 0;
        }
        uint256 accrued = accruedInterest(loanId);
        // Saturating rather than checked: collection may legitimately
        // record interest up to the frozen snapshot, and an underflow here
        // would make every later read revert.
        uint256 interestDue =
            accrued > loan.interestCollected ? accrued - loan.interestCollected : 0;
        return loan.principalOutstanding + interestDue;
    }

    // --- Repayment ---

    /// @notice Repay principal and accrued interest in full.
    /// @dev Callable while paused. Pulls from the credit account, so neither
    /// agent nor owner needs withdrawal rights to settle the debt.
    function repay(uint256 loanId) external {
        Loan storage loan = loans[loanId];
        require(loan.status == Status.Active, WrongStatus(Status.Active, loan.status));

        uint256 interestDue = accruedInterest(loanId) - loan.interestCollected;
        uint256 principal = loan.principalOutstanding;

        CreditAccount(loan.account).sweepTo(address(asset), address(vault), principal + interestDue);

        loan.principalOutstanding = 0;
        loan.interestCollected += interestDue;
        loan.status = Status.Repaid;
        activeLoan[loan.account] = 0;

        vault.recordPrincipalRepaid(loan.account, principal);
        vault.recordInterestCollected(loan.account, interestDue);
        CreditAccount(loan.account).closeDebt();

        emit Repaid(loanId, principal, interestDue);
    }

    // --- Recovery ---

    /// @notice Open recovery on a health breach or at maturity.
    /// @dev Snapshots accrued interest so later partial collections need no
    /// new interest schedule (`docs/03`).
    function openRecovery(uint256 loanId, address keeper) external onlyOperator {
        Loan storage loan = loans[loanId];
        require(loan.status == Status.Active, WrongStatus(Status.Active, loan.status));

        loan.interestSnapshot = _accruedAt(loan, uint64(block.timestamp));
        loan.recoveryStartedAt = uint64(block.timestamp);
        loan.status = Status.RecoveryPending;

        vault.markReceivableUnresolved(loan.principalOutstanding);
        CreditAccount(loan.account).openRecovery(keeper);

        emit RecoveryOpened(loanId, loan.recoveryStartedAt);
    }

    /// @notice Apply recovered proceeds. Idempotent across several calls.
    /// @dev `docs/05`: collection accounting must be idempotent, because
    /// recovery may collect proceeds in several transactions. Principal is
    /// repaid before interest.
    function collect(uint256 loanId, uint256 amount) external {
        Loan storage loan = loans[loanId];
        require(
            loan.status == Status.RecoveryPending,
            WrongStatus(Status.RecoveryPending, loan.status)
        );
        require(amount > 0, ZeroAmount());
        // Only the appointed keeper collects. An open recovery would
        // otherwise let any caller move the account's balance.
        require(
            msg.sender == CreditAccount(loan.account).recoveryKeeper()
                || msg.sender == operator,
            NotKeeper()
        );

        // Never take more than the loan is owed. The surplus is the
        // borrower's residual equity (`docs/02`), and sweeping it would
        // confiscate it -- and push interestCollected above what accrued.
        uint256 owed = debtDue(loanId);
        if (amount > owed) {
            amount = owed;
        }
        require(amount > 0, ZeroAmount());

        CreditAccount(loan.account).sweepTo(address(asset), address(vault), amount);

        uint256 toPrincipal = amount > loan.principalOutstanding
            ? loan.principalOutstanding
            : amount;
        uint256 toInterest = amount - toPrincipal;

        if (toPrincipal > 0) {
            loan.principalOutstanding -= toPrincipal;
            vault.recordPrincipalRepaid(loan.account, toPrincipal);
            vault.clearReceivable(toPrincipal);
        }
        if (toInterest > 0) {
            loan.interestCollected += toInterest;
            vault.recordInterestCollected(loan.account, toInterest);
        }

        emit Collected(loanId, toPrincipal, toInterest);

        if (loan.principalOutstanding == 0 && loan.interestCollected >= loan.interestSnapshot) {
            loan.status = Status.Recovered;
            activeLoan[loan.account] = 0;
            CreditAccount(loan.account).closeDebt();
            emit Recovered(loanId);
        }
    }

    /// @notice Close a loan whose assets are exhausted, allocating the loss.
    /// @dev Operator-gated and deliberately manual. `docs/05`: unavailable
    /// liquidity leaves a loan RecoveryPending — it is never resolved because
    /// a timeout elapsed or an optimistic price exists.
    function declareDefault(uint256 loanId) external onlyOperator {
        Loan storage loan = loans[loanId];
        require(
            loan.status == Status.RecoveryPending,
            WrongStatus(Status.RecoveryPending, loan.status)
        );
        require(CreditAccount(loan.account).balanceOf(address(asset)) == 0, NotYetDefaultable());

        uint256 principalLoss = loan.principalOutstanding;
        uint256 unpaidInterest = loan.interestSnapshot - loan.interestCollected;

        loan.principalOutstanding = 0;
        loan.status = Status.Defaulted;
        activeLoan[loan.account] = 0;
        // The loss stays attributable to this binding.
        defaulted[loan.account] = true;

        if (principalLoss > 0) {
            vault.recordLoss(loan.account, principalLoss);
            vault.clearReceivable(principalLoss);
        }
        CreditAccount(loan.account).closeDebt();

        emit Defaulted(loanId, principalLoss, unpaidInterest);
    }

    function status(uint256 loanId) external view returns (Status) {
        return loans[loanId].status;
    }
}
