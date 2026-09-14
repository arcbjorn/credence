// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {MockERC20} from "./MockERC20.sol";

/// Adapter that does nothing but succeed — stands in for a strategy route.
contract OkAdapter {
    uint256 public calls;

    function act() external {
        calls++;
    }

    /// A second entry point, so a test can present a call the assessment
    /// never authorized through an adapter that IS allowlisted.
    function other() external {
        calls++;
    }
}

/// Adapter that calls back into `execute` during its own call.
///
/// Stands in for a compromised route. `stateVersion` is what invalidates a
/// standing authorization, so bumping it AFTER the external call would let
/// a reentrant second action run against the very version that authorized
/// the first.
contract ReentrantAdapter {
    uint256 public calls;
    uint256 public versionSeenOnReentry;
    bool public reentered;
    bool public reentryReverted;
    address public account;

    function arm(address account_) external {
        account = account_;
    }

    function act() external {
        calls++;
        if (account == address(0) || reentered) return;
        reentered = true;
        versionSeenOnReentry = CreditAccount(account).stateVersion();
        try CreditAccount(account)
            .execute(address(this), abi.encodeWithSelector(this.act.selector)) {
        // Recorded so the test can assert on it.
        }
        catch {
            reentryReverted = true;
        }
    }
}

/// Adapter that tries to pull tokens out to an arbitrary recipient, standing
/// in for a malicious or compromised route.
contract DrainAdapter {
    function drain(address token, address account, address to, uint256 amount) external {
        CreditAccount(account).withdraw(token, to, amount);
    }
}

/// Stands in for the oracle so a test can decide whether acting is
/// permitted without assembling a signed assessment. Defaults to allowed,
/// which is the setting under which the OTHER invariants must still hold.
contract StubOracle {
    bool public allowed = true;
    /// The newest accepted sequence. A test raises this to simulate a
    /// newer assessment superseding the origination quote.
    uint64 public accepted;

    function setAllowed(bool v) external {
        allowed = v;
    }

    function setAccepted(uint64 v) external {
        accepted = v;
    }

    function lastSequence(bytes32) external view returns (uint64) {
        return accepted;
    }

    function riskAllowedFor(bytes32, bytes32, uint64) external view returns (bool) {
        return allowed;
    }
}

contract CreditAccountTest is Test {
    CreditAccount account;
    MockERC20 token;
    OkAdapter adapter;
    StubOracle oracle;

    address constant LOAN_MANAGER = address(0xA1);
    address constant OWNER = address(0xB2);
    address constant AGENT = address(0xC3);
    address constant KEEPER = address(0xD4);
    address constant OUTSIDER = address(0xE5);

    /// Stand-in binding identity for a pinned loan. The account only ever
    /// compares these, so their values matter less than their consistency.
    bytes32 constant KEY = keccak256("binding-key");
    bytes32 constant BINDING_HASH = keccak256("binding-hash");

    /// The sequence a pinned quote was signed at. Anything above this is
    /// a newer assessment that supersedes it.
    uint64 constant QUOTE_SEQ = 5;

    /// The action a pinned loan permits: one call to the test adapter.
    function _actionHash() internal view returns (bytes32) {
        return keccak256(abi.encode(address(adapter), abi.encodeCall(OkAdapter.act, ())));
    }

    function setUp() public {
        oracle = new StubOracle();
        account = new CreditAccount(LOAN_MANAGER, OWNER, AGENT, address(oracle));
        token = new MockERC20();
        adapter = new OkAdapter();

        token.mint(address(account), 10_000e6);
        vm.prank(LOAN_MANAGER);
        account.setAdapter(address(adapter), true);
    }

    // --- Invariant 2: the load-bearing rule ---

    /// `docs/09` beat 6 and `docs/11`: a prohibited withdrawal must be
    /// rejected by the contract. This is the single most convincing moment
    /// in the demo, so it is the first thing tested.
    function test_withdrawBlockedWhileDebtOutstanding() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        vm.prank(OWNER);
        vm.expectRevert(CreditAccount.WithdrawalBlockedByDebt.selector);
        account.withdraw(address(token), OWNER, 1e6);
    }

    /// The owner is bound exactly as the agent is. `docs/02` is explicit that
    /// ownership provides no withdrawal bypass.
    function test_ownerHasNoBypass() public {
        // The drain is registered AND pinned as the permitted action, so
        // this test reaches the laundering defence itself rather than
        // stopping at the authorization gate in front of it.
        DrainAdapter drain = new DrainAdapter();
        bytes memory drainCall =
            abi.encodeCall(DrainAdapter.drain, (address(token), address(account), OUTSIDER, 1e6));

        vm.startPrank(LOAN_MANAGER);
        account.openDebt(
            KEY,
            BINDING_HASH,
            keccak256(abi.encode(address(drain), drainCall)),
            uint64(block.timestamp + 1 hours),
            QUOTE_SEQ
        );
        account.setAdapter(address(drain), true);
        vm.stopPrank();

        // Owner is the only party who may ever withdraw, and still cannot.
        vm.prank(OWNER);
        vm.expectRevert(CreditAccount.WithdrawalBlockedByDebt.selector);
        account.withdraw(address(token), OWNER, 1);

        // A fully authorized adapter call still cannot move funds out: the
        // account never approved the drain, so the transfer itself fails.
        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.AdapterCallFailed.selector);
        account.execute(address(drain), drainCall);
    }

    function test_withdrawAllowedAfterDebtClosed() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        account.closeDebt();
        vm.stopPrank();

        vm.prank(OWNER);
        account.withdraw(address(token), OWNER, 1e6);
        assertEq(token.balanceOf(OWNER), 1e6);
    }

    function test_agentCannotWithdrawEverEvenWithoutDebt() public {
        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.NotOwnerOrAgent.selector);
        account.withdraw(address(token), AGENT, 1e6);
    }

    // --- Invariant 1: whitelisted adapters only ---

    function test_unregisteredAdapterRejected() public {
        OkAdapter rogue = new OkAdapter();
        vm.prank(AGENT);
        vm.expectRevert(
            abi.encodeWithSelector(CreditAccount.AdapterNotAllowed.selector, address(rogue))
        );
        account.execute(address(rogue), abi.encodeCall(OkAdapter.act, ()));
    }

    function test_outsiderCannotExecute() public {
        vm.prank(OUTSIDER);
        vm.expectRevert(CreditAccount.NotOwnerOrAgent.selector);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }

    function test_onlyLoanManagerSetsAdapters() public {
        vm.prank(OWNER);
        vm.expectRevert(CreditAccount.NotLoanManager.selector);
        account.setAdapter(address(adapter), true);
    }

    // --- State version ---

    /// `docs/05`: execution increments the state version so a prior
    /// assessment's action assumptions cannot be replayed.
    function test_executeIncrementsStateVersion() public {
        assertEq(account.stateVersion(), 0);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(account.stateVersion(), 1);
        assertEq(adapter.calls(), 1);
    }

    /// A reentrant adapter must not act twice on one authorization.
    ///
    /// `stateVersion` is what invalidates a standing action assumption, so
    /// it has to move BEFORE the external call. Bumping it afterwards
    /// leaves the re-entering call looking at the same version the first
    /// one was permitted under -- checks-effects-interactions, the same
    /// ordering `repay` already uses.
    ///
    /// Reported by review, 14 September.
    function test_reentrantAdapterSeesANewStateVersion() public {
        ReentrantAdapter bad = new ReentrantAdapter();
        vm.prank(LOAN_MANAGER);
        account.setAdapter(address(bad), true);
        bad.arm(address(account));

        uint256 before = account.stateVersion();
        vm.prank(AGENT);
        account.execute(address(bad), abi.encodeWithSelector(bad.act.selector));

        assertTrue(bad.reentered(), "the adapter did try to reenter");
        assertGt(
            bad.versionSeenOnReentry(),
            before,
            "the reentrant call must see a version the first call invalidated"
        );
    }

    // --- Recovery ---

    /// `docs/02`: recovery must reduce risk even below normal thresholds, or
    /// the health check itself prevents liquidation.
    function test_keeperRecoversOnlyWhenRecoveryOpen() public {
        vm.prank(KEEPER);
        vm.expectRevert(CreditAccount.RecoveryNotOpen.selector);
        account.recover(address(adapter), abi.encodeCall(OkAdapter.act, ()));

        vm.startPrank(LOAN_MANAGER);
        // A real deployment labels the unwind route before the loan goes
        // live; recovery may only take entry points declared risk-reducing.
        account.setDeRiskingAction(address(adapter), OkAdapter.act.selector, true);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        account.openRecovery(KEEPER);
        vm.stopPrank();

        vm.prank(KEEPER);
        account.recover(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1);
    }

    /// A keeper may not ACQUIRE through the route it was appointed to
    /// liquidate.
    ///
    /// Recovery is an exception to the permission gate, not to the
    /// direction of travel: a keeper reaching any allowlisted adapter could
    /// spend the borrower's residual equity on a position nobody assessed.
    /// Reported by review, 14 September.
    function test_keeperCannotActThroughARiskIncreasingEntryPoint() public {
        vm.startPrank(LOAN_MANAGER);
        account.setDeRiskingAction(address(adapter), OkAdapter.act.selector, true);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        account.openRecovery(KEEPER);
        vm.stopPrank();

        // `other()` is on the SAME allowlisted adapter, and was never
        // declared risk-reducing.
        vm.prank(KEEPER);
        vm.expectRevert(
            abi.encodeWithSelector(CreditAccount.NotRiskReducing.selector, address(adapter))
        );
        account.recover(address(adapter), abi.encodeCall(OkAdapter.other, ()));
    }

    /// Calldata too short to carry a selector names no entry point at all.
    function test_keeperCannotRecoverWithoutASelector() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        account.openRecovery(KEEPER);
        vm.stopPrank();

        vm.prank(KEEPER);
        vm.expectRevert(
            abi.encodeWithSelector(CreditAccount.NotRiskReducing.selector, address(adapter))
        );
        account.recover(address(adapter), hex"1122");
    }

    function test_nonKeeperCannotRecover() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        account.openRecovery(KEEPER);
        vm.stopPrank();

        vm.prank(OWNER);
        vm.expectRevert(CreditAccount.NotKeeper.selector);
        account.recover(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }

    /// Repayment stays open during recovery — `docs/05` requires that a pause
    /// or recovery never blocks repayment.
    function test_loanManagerSweepsDuringRecovery() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        account.openRecovery(KEEPER);
        account.sweepTo(address(token), LOAN_MANAGER, 5_000e6);
        vm.stopPrank();
        assertEq(token.balanceOf(LOAN_MANAGER), 5_000e6);
    }

    function test_outsiderCannotSweep() public {
        vm.prank(OUTSIDER);
        vm.expectRevert(CreditAccount.NotLoanManager.selector);
        account.sweepTo(address(token), OUTSIDER, 1e6);
    }

    // --- Invariant 3: execution needs live permission (docs/02, docs/05) ---

    /// An allowlisted adapter is not authorization.
    ///
    /// The allowlist says the route is known. Whether acting is safe right
    /// now is the engine's judgment, expressed as standing permission. A
    /// loan whose permission has lapsed must not keep trading.
    function test_ExecuteRequiresStandingPermission() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        // The first action is the one origination authorized, so it runs
        // on that authority alone.
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));

        // Anything after it is a new risk decision. Re-pinning the same
        // action isolates the permission gate from the action gate.
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));

        oracle.setAllowed(false);
        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.RiskNotPermitted.selector);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }

    /// The action origination authorized is single-use. A second identical
    /// call is a new risk decision and needs live permission.
    function test_PermittedActionIsSingleUse() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));

        oracle.setAllowed(false);
        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.RiskNotPermitted.selector);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }

    /// Only the assessed action may run. A permitted call is authorization
    /// for that call, not for any call through the same adapter.
    function test_ExecuteRefusesAnUnassessedAction() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        // Same adapter, different call.
        bytes memory other = abi.encodeCall(OkAdapter.other, ());
        vm.prank(AGENT);
        vm.expectRevert(
            abi.encodeWithSelector(
                CreditAccount.ActionNotPermitted.selector,
                _actionHash(),
                keccak256(abi.encode(address(adapter), other))
            )
        );
        account.execute(address(adapter), other);
    }

    /// The assessed action, with permission standing, still runs.
    function test_ExecutePermitsTheAssessedAction() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1, "the assessed action must still run");
    }

    /// An account with no debt is unrestricted: there is no lender
    /// exposure to protect, and requiring a quote to move one's own equity
    /// would make the account unusable before its first loan.
    function test_UnfundedAccountNeedsNoPermission() public {
        oracle.setAllowed(false);

        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1, "an unfunded account acts freely");
    }

    /// Recovery belongs to the keeper. An owner or agent who could keep
    /// trading would be racing the liquidation they are being recovered
    /// from.
    function test_ExecuteBlockedDuringRecovery() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        account.openRecovery(KEEPER);
        vm.stopPrank();

        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.ExecutionBlockedByRecovery.selector);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }

    /// A fresh monitoring result must authorize the NEXT action.
    ///
    /// The binding's identity is immutable; its account state version is
    /// not. Committing to both together means a correctly signed
    /// monitoring assessment -- which carries the NEW version -- can never
    /// match the hash pinned at origination, and the account is
    /// permanently unable to act after one execution.
    function test_FreshMonitoringAuthorizesTheNextAction() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        // First action runs on the origination quote.
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(account.stateVersion(), 1, "the action advanced the version");

        // A monitoring result arrives for the account AS IT NOW STANDS.
        // The stub answers for whatever version it is asked about, which
        // is what a correctly signed assessment would do.
        vm.prank(LOAN_MANAGER);
        account.setDeRiskingAction(address(adapter), OkAdapter.other.selector, false);

        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 2, "a fresh permission must allow the next action");
    }

    /// The first action is covered by the origination quote only WHILE
    /// THAT QUOTE IS LIVE.
    ///
    /// Origination and execution are separate transactions. Without the
    /// deadline a borrower can wait out the quote, receive an explicit
    /// Blocked, and still execute on authority that lapsed.
    function test_FirstActionDoesNotSurviveQuoteExpiry() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        // The quote lapses, and monitoring explicitly blocks.
        vm.warp(block.timestamp + 2 hours);
        oracle.setAllowed(false);

        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.RiskNotPermitted.selector);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }

    /// Inside the window, the first action still runs without a separate
    /// monitoring result -- otherwise no loan could deploy the principal
    /// it was just granted.
    function test_FirstActionRunsInsideTheQuoteWindow() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        oracle.setAllowed(false);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1, "a live quote covers its own action");
    }

    /// A newer Blocked result revokes quote coverage BEFORE expiry.
    ///
    /// The previous fix bounded the first action by the quote's deadline.
    /// That is not enough: every accepted assessment supersedes unused
    /// older quotes (`docs/05`), so a Blocked monitoring result arriving
    /// inside the window must stop the action too. Otherwise a borrower
    /// reads its own revocation and executes anyway.
    function test_NewerBlockRevokesQuoteCoverageBeforeExpiry() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        // Well inside the window, so expiry is not what stops this.
        vm.warp(block.timestamp + 10 minutes);

        // A newer assessment arrives and blocks.
        oracle.setAccepted(QUOTE_SEQ + 1);
        oracle.setAllowed(false);

        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.RiskNotPermitted.selector);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }

    /// A newer ALLOWED result lets the action through, so supersession
    /// revokes the quote's coverage rather than the permission itself.
    function test_NewerAllowRevokesCoverageButPermits() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        vm.warp(block.timestamp + 10 minutes);
        oracle.setAccepted(QUOTE_SEQ + 1);
        // allowed stays true: the newer assessment permits.

        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1, "a newer allowance must still permit");
    }

    /// With nothing newer accepted, the quote still covers its own action
    /// — otherwise no loan could deploy the principal it was granted.
    function test_QuoteStillCoversWhenNothingIsNewer() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt(
            KEY, BINDING_HASH, _actionHash(), uint64(block.timestamp + 1 hours), QUOTE_SEQ
        );

        oracle.setAccepted(QUOTE_SEQ);
        oracle.setAllowed(false);

        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1, "an unsuperseded quote covers its action");
    }
}
