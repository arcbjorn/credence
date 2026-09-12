// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {MockERC20} from "./MockERC20.sol";

/// Adapter that does nothing but succeed — stands in for a strategy route.
contract OkAdapter {
    uint256 public calls;
    function act() external { calls++; }
}

/// Adapter that tries to pull tokens out to an arbitrary recipient, standing
/// in for a malicious or compromised route.
contract DrainAdapter {
    function drain(address token, address account, address to, uint256 amount) external {
        CreditAccount(account).withdraw(token, to, amount);
    }
}

contract CreditAccountTest is Test {
    CreditAccount account;
    MockERC20 token;
    OkAdapter adapter;

    address constant LOAN_MANAGER = address(0xA1);
    address constant OWNER = address(0xB2);
    address constant AGENT = address(0xC3);
    address constant KEEPER = address(0xD4);
    address constant OUTSIDER = address(0xE5);

    function setUp() public {
        account = new CreditAccount(LOAN_MANAGER, OWNER, AGENT);
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
        account.openDebt();

        vm.prank(OWNER);
        vm.expectRevert(CreditAccount.WithdrawalBlockedByDebt.selector);
        account.withdraw(address(token), OWNER, 1e6);
    }

    /// The owner is bound exactly as the agent is. `docs/02` is explicit that
    /// ownership provides no withdrawal bypass.
    function test_ownerHasNoBypass() public {
        vm.prank(LOAN_MANAGER);
        account.openDebt();

        // Owner is the only party who may ever withdraw, and still cannot.
        vm.prank(OWNER);
        vm.expectRevert(CreditAccount.WithdrawalBlockedByDebt.selector);
        account.withdraw(address(token), OWNER, 1);

        // An adapter cannot launder the call either.
        DrainAdapter drain = new DrainAdapter();
        vm.prank(LOAN_MANAGER);
        account.setAdapter(address(drain), true);

        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.AdapterCallFailed.selector);
        account.execute(
            address(drain),
            abi.encodeCall(DrainAdapter.drain, (address(token), address(account), OUTSIDER, 1e6))
        );
    }

    function test_withdrawAllowedAfterDebtClosed() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt();
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
        account.openDebt();
        account.openRecovery(KEEPER);
        vm.stopPrank();

        vm.prank(KEEPER);
        account.recover(address(adapter), abi.encodeCall(OkAdapter.act, ()));
        assertEq(adapter.calls(), 1);
    }

    function test_nonKeeperCannotRecover() public {
        vm.startPrank(LOAN_MANAGER);
        account.openDebt();
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
        account.openDebt();
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
}
