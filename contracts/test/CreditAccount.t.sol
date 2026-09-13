// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {MockERC20} from "./MockERC20.sol";

/// Adapter that does nothing but succeed — stands in for a strategy route.
contract OkAdapter {
    uint256 public calls;
    function act() external { calls++; }
    /// A second entry point, so a test can present a call the assessment
    /// never authorized through an adapter that IS allowlisted.
    function other() external { calls++; }
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

    function setAllowed(bool v) external {
        allowed = v;
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
        account.openDebt(KEY, BINDING_HASH, _actionHash());

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
        bytes memory drainCall = abi.encodeCall(
            DrainAdapter.drain, (address(token), address(account), OUTSIDER, 1e6)
        );

        vm.startPrank(LOAN_MANAGER);
        account.openDebt(KEY, BINDING_HASH, keccak256(abi.encode(address(drain), drainCall)));
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
        account.openDebt(KEY, BINDING_HASH, _actionHash());
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

    // --- Recovery ---

    /// `docs/02`: recovery must reduce risk even below normal thresholds, or
    /// the health check itself prevents liquidation.
    function test_keeperRecoversOnlyWhenRecoveryOpen() public {
        vm.prank(KEEPER);
        vm.expectRevert(CreditAccount.RecoveryNotOpen.selector);
        account.recover(address(adapter), abi.encodeCall(OkAdapter.act, ()));

        vm.startPrank(LOAN_MANAGER);
        account.openDebt(KEY, BINDING_HASH, _actionHash());
        account.openRecovery(KEEPER);
        vm.stopPrank();

        vm.prank(KEEPER);
        account.recover(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1);
    }

    function test_nonKeeperCannotRecover() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt(KEY, BINDING_HASH, _actionHash());
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
        account.openDebt(KEY, BINDING_HASH, _actionHash());
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
        account.openDebt(KEY, BINDING_HASH, _actionHash());

        // The first action is the one origination authorized, so it runs
        // on that authority alone.
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));

        // Anything after it is a new risk decision. Re-pinning the same
        // action isolates the permission gate from the action gate.
        vm.prank(LOAN_MANAGER);
        account.openDebt(KEY, BINDING_HASH, _actionHash());
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
        account.openDebt(KEY, BINDING_HASH, _actionHash());

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
        account.openDebt(KEY, BINDING_HASH, _actionHash());

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
        account.openDebt(KEY, BINDING_HASH, _actionHash());

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
        account.openDebt(KEY, BINDING_HASH, _actionHash());
        account.openRecovery(KEEPER);
        vm.stopPrank();

        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.ExecutionBlockedByRecovery.selector);
        account.execute(address(adapter), abi.encodeCall(OkAdapter.act, ()));
    }
}
