// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {Assessment, Binding, Class, Decision, Kind, Permission, Route, Terms} from "../src/Assessment.sol";
import {IERC20} from "../src/IERC20.sol";
import {MockERC20} from "./MockERC20.sol";
import {OracleHarness} from "./OracleHarness.sol";

/// Drives the protocol through random sequences. Only the operator-permitted
/// paths are exposed, so the invariants describe what must hold under any
/// legitimate ordering — not what holds when arbitrary storage is written.
contract LifecycleHandler is Test {
    MockERC20 public token;
    CreditVault public vault;
    LoanManager public manager;
    CreditAccount public account;
    RiskOracle public oracle;
    address public immutable operator;
    uint256 constant SIGNER_KEY = 0xD30;
    uint256 constant DEPLOYMENT = 7;
    uint256 constant FACILITY = 1;
    uint64 public seq;

    uint256 public loanId;
    uint256 public ghostLent;
    uint256 public ghostPrincipalReturned;

    constructor(
        MockERC20 token_,
        CreditVault vault_,
        LoanManager manager_,
        CreditAccount account_,
        RiskOracle oracle_,
        address operator_
    ) {
        token = token_;
        vault = vault_;
        manager = manager_;
        account = account_;
        oracle = oracle_;
        operator = operator_;
    }

    function originate(uint256 principal) external {
        if (manager.activeLoan(address(account)) != 0) return;
        if (manager.defaulted(address(account))) return;
        if (vault.phase() != CreditVault.Phase.Active) return;
        principal = bound(principal, 1e6, 10_000e6);
        if (principal > vault.idleCash()) return;

        Binding memory b =
            OracleHarness.binding(address(account), account.stateVersion(), FACILITY);
        Assessment memory a = OracleHarness.approvedQuote(
            b, ++seq, principal, 1_200, 10 days, 0, address(token), DEPLOYMENT,
            uint64(block.timestamp)
        );
        oracle.accept(a, OracleHarness.sign(oracle, a, SIGNER_KEY));
        loanId = manager.originate(b, principal);
        ghostLent += principal;
    }

    function passTime(uint256 secs) external {
        vm.warp(block.timestamp + bound(secs, 1, 30 days));
    }

    function repay() external {
        if (loanId == 0) return;
        if (manager.status(loanId) != LoanManager.Status.Active) return;
        uint256 due = manager.debtDue(loanId);
        if (token.balanceOf(address(account)) < due) return;

        uint256 principalBefore = vault.outstandingPrincipal();
        manager.repay(loanId);
        ghostPrincipalReturned += principalBefore - vault.outstandingPrincipal();
        loanId = 0;
    }

    /// The keeper appointed when recovery opens. Named so collection
    /// pranks the same address rather than drifting from it.
    address constant KEEPER = address(0xD4);

    function openRecovery() external {
        if (loanId == 0) return;
        if (manager.status(loanId) != LoanManager.Status.Active) return;
        Binding memory rb =
            OracleHarness.binding(address(account), account.stateVersion(), FACILITY);
        vm.prank(operator);
        manager.openRecovery(loanId, KEEPER, rb);
    }

    function collect(uint256 amount) external {
        if (loanId == 0) return;
        if (manager.status(loanId) != LoanManager.Status.RecoveryPending) return;
        uint256 available = token.balanceOf(address(account));
        if (available == 0) return;
        amount = bound(amount, 1, available);

        uint256 principalBefore = vault.outstandingPrincipal();
        vm.prank(KEEPER);
        manager.collect(loanId, amount);
        ghostPrincipalReturned += principalBefore - vault.outstandingPrincipal();
        if (manager.status(loanId) == LoanManager.Status.Recovered) loanId = 0;
    }

    /// Write off what recovery could not collect.
    ///
    /// Without this the fuzzer never reaches a realized loss, so
    /// `invariant_principalConserved` carried a `realizedLoss()` term that
    /// was always zero -- the conservation claim was only ever tested on
    /// the paths where nothing was lost. Reported by review, 14 September.
    function declareDefault() external {
        if (loanId == 0) return;
        if (manager.status(loanId) != LoanManager.Status.RecoveryPending) return;
        // A default requires every asset exhausted, not merely a shortfall
        // (`docs/05` invariant 9). Sweep what remains so the write-off is
        // reached rather than reverted.
        uint256 remaining = token.balanceOf(address(account));
        if (remaining > 0) {
            // Credited to the ghost exactly as the standalone `collect`
            // action does: this really is principal coming back, and
            // omitting it here loses it from the identity.
            uint256 principalBefore = vault.outstandingPrincipal();
            vm.prank(KEEPER);
            manager.collect(loanId, remaining);
            ghostPrincipalReturned += principalBefore - vault.outstandingPrincipal();
            if (manager.status(loanId) == LoanManager.Status.Recovered) {
                loanId = 0;
                return;
            }
        }

        vm.prank(operator);
        manager.declareDefault(loanId);
        // No ghost adjustment. The identity is
        //   lent == returned + outstanding + realizedLoss
        // and a write-off moves principal from `outstanding` straight into
        // `realizedLoss`, leaving both sides unchanged. Crediting it to
        // `returned` as well would count it twice.
        loanId = 0;
    }
}

contract InvariantsTest is Test {
    MockERC20 token;
    CreditVault vault;
    RiskOracle oracle;
    LoanManager manager;
    CreditAccount account;
    LifecycleHandler handler;

    address constant OPERATOR = address(0xA1);

    function setUp() public {
        token = new MockERC20();
        vault = new CreditVault(IERC20(address(token)), OPERATOR, 100_000e6);
        oracle = new RiskOracle(OPERATOR, 7, Class.PrototypeEligible);
        manager = new LoanManager(IERC20(address(token)), vault, oracle, OPERATOR);
        vm.startPrank(OPERATOR);
        vault.setLoanManager(address(manager));
        oracle.setLoanManager(address(manager));
        oracle.pinRoute(1, Route.Development);
        oracle.setSigner(Route.Development, vm.addr(0xD30), true);
        vm.stopPrank();
        account = new CreditAccount(address(manager), address(0xB2), address(0xC3), address(oracle));

        address lender = address(0xE5);
        token.mint(lender, 50_000e6);
        vm.startPrank(lender);
        token.approve(address(vault), 50_000e6);
        vault.deposit(50_000e6);
        vm.stopPrank();
        vm.prank(OPERATOR);
        vault.activate();

        token.mint(address(account), 20_000e6);

        handler = new LifecycleHandler(token, vault, manager, account, oracle, OPERATOR);
        targetContract(address(handler));
    }

    /// Cash held must always cover what the ledgers say is free. Idle cash is
    /// a claim on real tokens; if it ever exceeds the balance, the vault has
    /// promised money it does not hold.
    function invariant_idleCashIsBacked() public view {
        assertGe(token.balanceOf(address(vault)), vault.idleCash());
    }

    /// Principal is conserved: everything lent has either come back, been
    /// written off, or is still outstanding. `docs/11` conservation of funds.
    function invariant_principalConserved() public view {
        assertEq(
            handler.ghostLent(),
            handler.ghostPrincipalReturned() + vault.outstandingPrincipal() + vault.realizedLoss()
        );
    }

    /// Outstanding principal never exceeds the global cap.
    function invariant_globalCapRespected() public view {
        assertLe(vault.outstandingPrincipal(), vault.globalCap());
    }

    /// Debt state tracks the loan: an account carries debt if and only if a
    /// loan is active or recovering against it.
    function invariant_debtFlagMatchesLoanState() public view {
        uint256 id = manager.activeLoan(address(account));
        if (id == 0) {
            assertFalse(account.debtOutstanding());
        } else {
            assertTrue(account.debtOutstanding());
        }
    }

    /// While debt is outstanding the owner cannot withdraw. This is the
    /// load-bearing rule (`docs/02`), asserted under every reachable state.
    function invariant_noWithdrawalWhileDebtOutstanding() public {
        if (!account.debtOutstanding()) return;
        vm.prank(address(0xB2));
        vm.expectRevert(CreditAccount.WithdrawalBlockedByDebt.selector);
        account.withdraw(address(token), address(0xB2), 1);
    }

    /// Collected interest only ever grows, and is only recorded against cash
    /// actually received.
    function invariant_collectedInterestIsRealized() public view {
        assertLe(vault.collectedInterest(), token.balanceOf(address(vault)));
    }
}

/// Guard against vacuous invariants: proves the handler actually drives the
/// protocol rather than having every call rejected by its own guards.
contract HandlerReachabilityTest is Test {
    MockERC20 token;
    CreditVault vault;
    RiskOracle oracle;
    LoanManager manager;
    CreditAccount account;
    LifecycleHandler handler;
    address constant OPERATOR = address(0xA1);

    function setUp() public {
        token = new MockERC20();
        vault = new CreditVault(IERC20(address(token)), OPERATOR, 100_000e6);
        oracle = new RiskOracle(OPERATOR, 7, Class.PrototypeEligible);
        manager = new LoanManager(IERC20(address(token)), vault, oracle, OPERATOR);
        vm.startPrank(OPERATOR);
        vault.setLoanManager(address(manager));
        oracle.setLoanManager(address(manager));
        oracle.pinRoute(1, Route.Development);
        oracle.setSigner(Route.Development, vm.addr(0xD30), true);
        vm.stopPrank();
        account = new CreditAccount(address(manager), address(0xB2), address(0xC3), address(oracle));

        address lender = address(0xE5);
        token.mint(lender, 50_000e6);
        vm.startPrank(lender);
        token.approve(address(vault), 50_000e6);
        vault.deposit(50_000e6);
        vm.stopPrank();
        vm.prank(OPERATOR);
        vault.activate();
        token.mint(address(account), 20_000e6);

        handler = new LifecycleHandler(token, vault, manager, account, oracle, OPERATOR);
    }

    function test_handlerReachesEveryState() public {
        handler.originate(5_000e6);
        assertGt(handler.ghostLent(), 0, "originate reachable");
        assertTrue(account.debtOutstanding());

        handler.passTime(10 days);
        handler.repay();
        assertGt(handler.ghostPrincipalReturned(), 0, "repay reachable");
        assertFalse(account.debtOutstanding());

        handler.originate(3_000e6);
        handler.passTime(10 days);
        handler.openRecovery();
        assertEq(
            uint8(manager.status(handler.loanId())),
            uint8(LoanManager.Status.RecoveryPending),
            "recovery reachable"
        );

        handler.collect(1_000e6);
        assertGt(vault.collectedInterest() + handler.ghostPrincipalReturned(), 0);
    }
}
