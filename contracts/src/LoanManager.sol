// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {IERC20} from "./IERC20.sol";
import {SafeTransfer} from "./SafeTransfer.sol";
import {CreditAccount} from "./CreditAccount.sol";

/// @dev The slice of an ERC-721 identity registry this contract needs.
interface IIdentityRegistry {
    function ownerOf(uint256 tokenId) external view returns (address);
}
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
        /// @dev When the loan reached a terminal status. Recorded because
        /// duration is a published fact (`docs/06`), and a publisher that
        /// stamped its own clock would report the time it got around to
        /// publishing rather than when the obligation actually ended.
        uint64 resolvedAt;
        /// @dev Principal written off at default, and interest never
        /// collected. Recorded because the terminal state zeroes
        /// `principalOutstanding`, so the loss cannot be recovered from it
        /// afterwards -- and published feedback reports realized loss
        /// (`docs/06`).
        uint256 principalLoss;
        uint256 unpaidInterest;
        Status status;
        /// @dev The agent this obligation belongs to, taken from the
        /// binding the quote was consumed under. Recorded because
        /// published feedback must be ATTRIBUTABLE (`docs/06`): a
        /// publisher that took an agent id from its own configuration
        /// would assert an attribution the chain never verified, and the
        /// same loan number under a different manager would carry the
        /// same claim. The id survives an identity transfer unchanged,
        /// which is what makes the loss stay attributable to the
        /// original binding.
        uint256 agentId;
    }

    uint256 private constant BPS_DENOM = 10_000;
    uint256 private constant SECONDS_PER_YEAR = 365 days;

    IERC20 public immutable asset;
    CreditVault public immutable vault;
    RiskOracle public immutable oracle;
    address public immutable operator;

    /// @notice How long a LAPSED permission is tolerated before the loan
    /// becomes recoverable on that ground alone.
    ///
    /// @dev Fixed per deployment rather than operator-tunable: the whole
    /// point is that the party who could withhold assessments cannot also
    /// shorten the window that withholding them opens.
    ///
    /// Twelve hours is chosen against the monitoring cadence, not the loan
    /// term -- long enough that an attestor outage or a missed cycle is
    /// not a seizure, short enough that an obligation nobody is assessing
    /// does not stay open indefinitely. An explicit `Blocked` is
    /// unaffected and triggers immediately.
    uint64 public constant recoveryGraceSeconds = 12 hours;

    /// @dev ERC-721 identity registry, when one is configured. A draw
    /// then requires the binding's `operator` to still own the agent
    /// token: `docs/06` freezes new credit on transfer, and a binding
    /// that names only the agent ID cannot notice one -- the ID survives
    /// a transfer unchanged.
    address public identityRegistry;

    uint256 public nextLoanId = 1;
    mapping(uint256 loanId => Loan) public loans;
    /// @dev One active loan per account (`docs/05`).
    mapping(address account => uint256 loanId) public activeLoan;
    /// @dev Assets that must be sold before a default may be declared.
    /// @dev Interest reserved against the cap for each live loan.
    mapping(uint256 loanId => uint256) public reservation;
    /// @dev Facility each loan was written under, so its obligation can be
    /// released against the same counter it opened.
    mapping(uint256 loanId => uint256) public loanFacility;
    mapping(address token => bool) public collateralAsset;
    address[] public collateralAssets;
    /// @dev Unresolved default flag; blocks new credit for that binding.
    mapping(address account => bool) public defaulted;

    bool public paused;

    error NotOperator();
    error AccountHasActiveLoan();
    error AccountInDefault();
    error WrongStatus(Status expected, Status actual);
    error NotYetDefaultable();
    error CollateralUnresolved(address token);
    error LoanNotRecoverable(uint256 loanId);
    error BindingNotForLoan(uint256 loanId);
    error IdentityTransferred(uint256 agentId);
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
    event CollateralAssetSet(address indexed token, bool isCollateral);
    event IdentityRegistrySet(address indexed registry);
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

    /// @notice Point the manager at an ERC-721 identity registry.
    /// @dev Empty disables the check, which is honest for a deployment
    /// with no registry rather than a silent pass.
    function setIdentityRegistry(address registry) external onlyOperator {
        identityRegistry = registry;
        emit IdentityRegistrySet(registry);
    }

    /// @notice Register a collateral asset recovery must resolve.
    /// @dev `docs/05`: a loan holding unsold illiquid positions stays
    /// RecoveryPending. Without a list of what counts as collateral, an
    /// empty settlement balance reads as "assets exhausted" while the
    /// account still holds the very positions recovery exists to sell.
    function setCollateralAsset(address token, bool isCollateral) external onlyOperator {
        collateralAsset[token] = isCollateral;
        if (isCollateral) {
            for (uint256 i = 0; i < collateralAssets.length; i++) {
                if (collateralAssets[i] == token) return;
            }
            collateralAssets.push(token);
        }
        emit CollateralAssetSet(token, isCollateral);
    }

    /// @notice Register an execution route on an account.
    /// @dev The account only accepts adapter changes from its loan manager,
    /// so this is the operator's path to whitelisting a strategy route.
    ///
    /// Refused while the account owes: the set of routes an agent may
    /// trade through is part of the terms it borrowed under. Revocation is
    /// always permitted, because withdrawing a route only ever reduces
    /// what the account can do.
    function setAccountAdapter(CreditAccount account, address adapter, bool allowed)
        external
        onlyOperator
    {
        require(
            !allowed || activeLoan[address(account)] == 0,
            AccountHasActiveLoan()
        );
        account.setAdapter(adapter, allowed);
    }

    /// @notice Declare an adapter entry point as risk-reducing.
    ///
    /// @dev Operator-gated for the same reason the adapter allowlist is:
    /// mislabelling a risk-INCREASING call as de-risking would exempt it
    /// from permission entirely.
    ///
    /// And refused while the account owes, on the same asymmetry
    /// `setAccountAdapter` uses (`docs/05` invariant 11). Granting an
    /// exemption mid-loan is the sharper version of adding a route: an
    /// adapter added while debt is live still needs standing permission
    /// to act, whereas a selector relabelled as de-risking needs none at
    /// all. Withdrawing a label stays open, because taking an exemption
    /// away only ever narrows what the account can do.
    ///
    /// Reported by review, 14 September.
    function setAccountDeRiskingAction(
        CreditAccount account,
        address adapter,
        bytes4 selector,
        bool isDeRisking
    ) external onlyOperator {
        require(
            !isDeRisking || activeLoan[address(account)] == 0,
            AccountHasActiveLoan()
        );
        account.setDeRiskingAction(adapter, selector, isDeRisking);
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
        public
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

        // The agent must still be owned by the party the assessment was
        // made for. An ERC-721 transfer preserves the token ID, so every
        // binding field survives it unchanged -- without this an old
        // quote stays drawable by whoever now controls the identity
        // (`docs/06`: freeze new credit on transfer).
        if (identityRegistry != address(0)) {
            require(
                IIdentityRegistry(identityRegistry).ownerOf(binding.agentId)
                    == binding.operator,
                IdentityTransferred(binding.agentId)
            );
        }

        // Consumes once, and only for this exact amount.
        (Terms memory terms, uint64 quoteValidUntil, uint64 quoteSequence) =
            oracle.consumeQuote(binding, principal);

        // `docs/02`: an agent with no equity receives no credit in v0.
        //
        // Enforced independently of the attestor's number. A signed
        // requiredEquity of zero would otherwise fund an empty account,
        // which makes the invariant a matter of who signed rather than a
        // property of the contract. Equity is the first loss layer; there
        // is no first loss layer without it.
        uint256 held = account.balanceOf(address(asset));
        require(held > 0, InsufficientEquity(1, 0));
        require(
            held >= terms.requiredEquity,
            InsufficientEquity(terms.requiredEquity, held)
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
            resolvedAt: 0,
            principalLoss: 0,
            unpaidInterest: 0,
            status: Status.Active,
            agentId: binding.agentId
        });
        activeLoan[acct] = loanId;

        // Pin what this loan authorizes. The account cannot derive these
        // itself -- it holds no binding and trusts no caller-supplied one
        // -- so authorization for every later action flows from here.
        // Reserve the full-term interest against the cap alongside the
        // principal, so the budget bounds what the loan will owe rather
        // than only what it takes today.
        uint256 reserved = _fullTermInterest(principal, terms.aprBps, terms.termSeconds);

        account.openDebt(
            oracle.bindingKey(binding),
            // The binding's immutable identity, not its full hash: the
            // state version changes with every execution and is checked
            // separately against the account's live value.
            oracle.bindingConfigHash(binding),
            terms.permittedActionHash,
            quoteValidUntil,
            quoteSequence
        );
        vault.lend(acct, principal, reserved);
        reservation[loanId] = reserved;
        loanFacility[loanId] = binding.facilityId;
        oracle.openObligation(binding.facilityId);

        emit Originated(loanId, acct, principal);
    }

    /// @notice Originate and perform the approved action in one transaction.
    ///
    /// @dev `docs/05`: "Where an initial trade is part of the approval,
    /// fund and trade atomically; revert everything if post-action checks
    /// fail."
    ///
    /// Two transactions leave a gap the assessment did not price. The
    /// engine sized this loan against the portfolio the action WOULD
    /// produce -- `IntendedAction::Acquire` with its deploy_bps -- so a
    /// borrower that drew and never executed would hold settlement tokens
    /// the stress scenarios assumed were deployed, and the equity backing
    /// its capacity would be doing nothing it was assessed as doing. That
    /// is not merely untidy; it is a different risk position than the one
    /// approved.
    ///
    /// The action is still checked by the account against
    /// `permittedActionHash`, exactly as it would be if called separately.
    /// Nothing here relaxes a rule; it removes the window between two that
    /// were always meant to hold together.
    ///
    /// `originate` remains for a quote whose approved action is to hold
    /// settlement tokens, where there is nothing to perform.
    ///
    /// Reported by review, 14 September.
    function originateAndExecute(
        Binding calldata binding,
        uint256 principal,
        address adapter,
        bytes calldata action
    ) external returns (uint256 loanId, bytes memory result) {
        loanId = originate(binding, principal);
        // Reverting here unwinds the draw with it: the vault's lend, the
        // debt record, and the account's lock all roll back together, so a
        // failed action leaves no funded loan behind.
        result = CreditAccount(loans[loanId].account).executeFor(
            msg.sender, adapter, action
        );
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

    /// @dev Interest a loan owes over its whole term. Mirrors
    /// `risk-core::interest::full_term`: 365-day year, rounds up.
    function _fullTermInterest(uint256 principal, uint32 aprBps, uint64 termSeconds)
        private
        pure
        returns (uint256)
    {
        uint256 numer = principal * aprBps * termSeconds;
        uint256 denom = BPS_DENOM * SECONDS_PER_YEAR;
        return numer == 0 ? 0 : (numer - 1) / denom + 1;
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

        // Effects BEFORE the transfer. A settlement token that calls back
        // during sweepTo would otherwise find this loan still Active with
        // its principal outstanding, repay a second time, and record the
        // principal twice at the vault -- underflowing outstandingPrincipal
        // on the way out (`docs/11`: reentrant callbacks cannot alter debt
        // or accounting before checks settle).
        loan.principalOutstanding = 0;
        loan.interestCollected += interestDue;
        loan.status = Status.Repaid;
        loan.resolvedAt = uint64(block.timestamp);
        activeLoan[loan.account] = 0;
        _releaseReservation(loanId);

        CreditAccount(loan.account).sweepTo(address(asset), address(vault), principal + interestDue);

        vault.recordPrincipalRepaid(loan.account, principal);
        vault.recordInterestCollected(loan.account, interestDue);
        _releaseAccount(loan.account);

        emit Repaid(loanId, principal, interestDue);
    }

    // --- Recovery ---

    /// @notice Open recovery on a health breach or at maturity.
    /// @dev Snapshots accrued interest so later partial collections need no
    /// new interest schedule (`docs/03`).
    /// @param binding The binding this loan was written under, so the
    /// oracle's standing permission can be consulted.
    ///
    /// @dev Recovery needs a reason, not merely an operator. Two qualify,
    /// and both are verifiable on chain rather than discretionary:
    ///
    ///   * **Maturity has passed.** The obligation is due and unpaid.
    ///   * **Permission is withdrawn.** The engine assessed the position
    ///     and blocked it, which is the covenant breach this protocol
    ///     actually has.
    ///
    /// Seizing a healthy, unmatured position would make every loan
    /// callable at will, which is not the instrument the borrower agreed
    /// to (`docs/05`).
    function openRecovery(uint256 loanId, address keeper, Binding calldata binding)
        external
        onlyOperator
    {
        Loan storage loan = loans[loanId];
        require(loan.status == Status.Active, WrongStatus(Status.Active, loan.status));

        // The binding must be THIS loan's. Without that, an operator can
        // present an unrelated agent -- one with no standing permission --
        // and liquidate an otherwise healthy, unmatured position, which
        // defeats the very restriction this check exists to impose.
        //
        // Compared against what the account pinned at origination, so the
        // caller cannot choose it.
        CreditAccount acct = CreditAccount(loan.account);
        require(
            oracle.bindingKey(binding) == acct.boundBindingKey()
                && oracle.bindingConfigHash(binding) == acct.boundBindingHash(),
            BindingNotForLoan(loanId)
        );

        // The state version too. bindingConfigHash deliberately excludes
        // it -- it changes on every execution and is checked separately --
        // but "separately" has to mean checked, not skipped. A fabricated
        // version makes riskAllowed return false, which reads here as
        // "permission withdrawn" and opens recovery on a healthy loan.
        require(
            binding.accountStateVersion == acct.stateVersion(),
            StaleAccountState(binding.accountStateVersion, acct.stateVersion())
        );

        bool matured = block.timestamp >= loan.originatedAt + loan.termSeconds;

        // Two different facts, deliberately not merged.
        //
        // An attestor that says "blocked" has made a statement about this
        // loan, and that is a trigger. An attestor that says NOTHING has
        // made no statement at all -- and treating silence the same way
        // would let an operator seize a healthy, unmatured position by
        // withholding updates, making the borrower's real covenant "keep
        // the attestor answering" rather than "stay solvent".
        //
        // A lapse still blocks new risk on its own (`docs/05`), and after
        // the grace window it still opens recovery: an obligation nobody
        // is assessing cannot stay open indefinitely. What it does not do
        // is become seizable the instant the last window closes.
        //
        // Reported by review, 14 September.
        bool withdrawn = oracle.permissionWithdrawn(binding);
        uint64 expiry = oracle.permissionExpiry(binding);

        // When no permission was ever issued, the grace window runs from
        // ORIGINATION rather than from an expiry that does not exist.
        //
        // Otherwise the two silences behave differently for no reason: a
        // loan monitored once and then abandoned becomes recoverable 12
        // hours after its window closes, while a loan never monitored at
        // all -- strictly less assessed -- could never be recovered.
        // Anchoring to origination gives the borrower the same protection
        // and the lender the same backstop in both cases.
        uint256 silenceSince = expiry != 0 ? uint256(expiry) : loan.originatedAt;
        bool lapsedBeyondGrace =
            block.timestamp >= silenceSince + recoveryGraceSeconds;

        require(
            matured || withdrawn || lapsedBeyondGrace,
            LoanNotRecoverable(loanId)
        );

        loan.interestSnapshot = _accruedAt(loan, uint64(block.timestamp));
        loan.recoveryStartedAt = uint64(block.timestamp);
        loan.status = Status.RecoveryPending;

        // The whole obligation is unresolved, not just principal. Marking
        // principal alone lets the epoch settle once principal is
        // collected while interest is still being recovered -- and that
        // interest can never be fairly included in a frozen
        // settlementCash (`docs/05`).
        uint256 unpaidInterest = loan.interestSnapshot - loan.interestCollected;
        vault.markReceivableUnresolved(loan.principalOutstanding + unpaidInterest);
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

        uint256 toPrincipal = amount > loan.principalOutstanding
            ? loan.principalOutstanding
            : amount;
        uint256 toInterest = amount - toPrincipal;

        // Effects before the transfer, for the same reason repay() orders
        // them that way: a settlement token that calls back during sweepTo
        // would otherwise see this loan's principal still outstanding and
        // collect against it twice.
        loan.principalOutstanding -= toPrincipal;
        loan.interestCollected += toInterest;

        CreditAccount(loan.account).sweepTo(address(asset), address(vault), amount);

        if (toPrincipal > 0) {
            vault.recordPrincipalRepaid(loan.account, toPrincipal);
            vault.clearReceivable(toPrincipal);
        }
        if (toInterest > 0) {
            vault.recordInterestCollected(loan.account, toInterest);
            vault.clearReceivable(toInterest);
        }

        emit Collected(loanId, toPrincipal, toInterest);

        if (loan.principalOutstanding == 0 && loan.interestCollected >= loan.interestSnapshot) {
            loan.status = Status.Recovered;
            loan.resolvedAt = uint64(block.timestamp);
            activeLoan[loan.account] = 0;
            _releaseReservation(loanId);
            _releaseAccount(loan.account);
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
        // Every asset must actually be exhausted, not merely the
        // settlement token. Declaring default while collateral remains
        // writes off lender principal and then unlocks those positions for
        // the owner to withdraw (`docs/05`).
        require(CreditAccount(loan.account).balanceOf(address(asset)) == 0, NotYetDefaultable());
        for (uint256 i = 0; i < collateralAssets.length; i++) {
            address token = collateralAssets[i];
            if (!collateralAsset[token]) continue;
            require(
                CreditAccount(loan.account).balanceOf(token) == 0,
                CollateralUnresolved(token)
            );
        }

        uint256 principalLoss = loan.principalOutstanding;
        uint256 unpaidInterest = loan.interestSnapshot - loan.interestCollected;
        // Retained before the write-off clears principalOutstanding.
        loan.principalLoss = principalLoss;
        loan.unpaidInterest = unpaidInterest;

        loan.principalOutstanding = 0;
        loan.status = Status.Defaulted;
        loan.resolvedAt = uint64(block.timestamp);
        activeLoan[loan.account] = 0;
        _releaseReservation(loanId);
        // The loss stays attributable to this binding.
        defaulted[loan.account] = true;

        if (principalLoss > 0) {
            vault.recordLoss(loan.account, principalLoss);
            vault.clearReceivable(principalLoss);
        }
        // Unpaid interest is written off with the principal. It will never
        // arrive, so leaving it marked unresolved would block the epoch
        // from ever settling.
        if (unpaidInterest > 0) {
            vault.clearReceivable(unpaidInterest);
        }
        _releaseAccount(loan.account);

        emit Defaulted(loanId, principalLoss, unpaidInterest);
    }

    /// @dev Releases once: a second call on a resolved loan would free
    /// budget that was never reserved.
    function _releaseReservation(uint256 loanId) private {
        // Released together: both are per-loan state that a terminal
        // status must clear exactly once.
        oracle.closeObligation(loanFacility[loanId]);

        uint256 amount = reservation[loanId];
        if (amount == 0) return;
        reservation[loanId] = 0;
        vault.releaseReservation(amount);
    }

    /// @dev Close the account's custody lock only when nothing is left
    /// owing on it.
    ///
    /// A terminal transition releases the account, but the account is
    /// shared by every loan written against it and only ONE may be active
    /// at a time. `repay` clears `activeLoan` before pulling settlement
    /// tokens, so a callback during that transfer sees a loan-free account
    /// and can draw against a standing quote -- and an unconditional
    /// `closeDebt()` on the way out would then release the NEW loan's
    /// lock, leaving the vault owed principal while the owner withdraws
    /// (`docs/05`: neither agent nor owner withdraws while debt is
    /// active). Reported by review, 14 September.
    ///
    /// Checking `activeLoan` rather than adding a reentrancy guard keeps
    /// the rule where the rule lives: the account is locked exactly while
    /// it owes, whatever order the calls arrived in.
    function _releaseAccount(address acct) private {
        if (activeLoan[acct] != 0) return;
        CreditAccount(acct).closeDebt();
    }

    function status(uint256 loanId) external view returns (Status) {
        return loans[loanId].status;
    }

    /// @notice The agent this obligation was originated for.
    /// @dev Read by the feedback publisher so attribution is DERIVED from
    /// the binding the loan was drawn under rather than supplied to the
    /// publisher (`docs/06`). Unchanged by an identity transfer, which is
    /// what keeps a realized loss attributable to the original binding.
    function agentIdOf(uint256 loanId) external view returns (uint256) {
        return loans[loanId].agentId;
    }
}
