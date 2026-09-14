// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {Assessment, Class, Binding, Route} from "../src/Assessment.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {IERC20} from "../src/IERC20.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {OracleHarness} from "./OracleHarness.sol";

/// A settlement token that calls back into the loan manager mid-transfer.
///
/// `docs/11`: reentrant callbacks cannot alter debt or accounting before
/// checks settle. The protocol's defence is checks-effects-interactions
/// ordering rather than a guard, which is the stronger design — but an
/// ordering claim is only as good as the attempt that tries to break it.
contract ReentrantToken {
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    LoanManager public target;
    uint256 public loanId;
    bool public attacking;
    uint256 public reentryAttempts;
    bytes public lastRevert;

    function mint(address to, uint256 amount) external {
        balanceOf[to] += amount;
    }

    /// When set, the callback collects instead of repaying.
    bool public collectMode;
    uint256 public collectAmount;

    function arm(LoanManager target_, uint256 loanId_) external {
        target = target_;
        loanId = loanId_;
        attacking = true;
    }

    function armCollect(LoanManager target_, uint256 loanId_, uint256 amount) external {
        target = target_;
        loanId = loanId_;
        collectAmount = amount;
        collectMode = true;
        attacking = true;
    }

    function _maybeReenter() private {
        if (!attacking || address(target) == address(0)) return;
        // Fire once: a loop would exhaust gas rather than prove anything.
        attacking = false;
        reentryAttempts++;
        // Repay again, mid-transfer, while the first repayment is still
        // settling. If ordering is wrong this sees a live loan.
        if (collectMode) {
            try target.collect(loanId, collectAmount) {
                // Recorded as a success so the test can assert on it.
            } catch (bytes memory reason) {
                lastRevert = reason;
            }
            return;
        }
        try target.repay(loanId) {
            // Recorded as a success so the test can assert on it.
        } catch (bytes memory reason) {
            lastRevert = reason;
        }
    }

    function transfer(address to, uint256 value) external returns (bool) {
        balanceOf[msg.sender] -= value;
        balanceOf[to] += value;
        _maybeReenter();
        return true;
    }

    function transferFrom(address from, address to, uint256 value) external returns (bool) {
        allowance[from][msg.sender] -= value;
        balanceOf[from] -= value;
        balanceOf[to] += value;
        _maybeReenter();
        return true;
    }

    function approve(address spender, uint256 value) external returns (bool) {
        allowance[msg.sender][spender] = value;
        return true;
    }
}

contract ReentrancyTest is Test {
    ReentrantToken token;
    CreditVault vault;
    RiskOracle oracle;
    LoanManager manager;
    CreditAccount account;

    address constant OPERATOR = address(0xA1);
    address constant OWNER = address(0xB2);
    address constant AGENT = address(0xC3);
    address constant LENDER = address(0xD4);
    uint256 constant DEPLOYMENT = 7;
    uint256 constant FACILITY = 1;
    uint256 constant SIGNER_KEY = 0xD30;
    uint64 seq;

    function setUp() public {
        token = new ReentrantToken();
        vault = new CreditVault(IERC20(address(token)), OPERATOR, 100_000e6);
        oracle = new RiskOracle(OPERATOR, DEPLOYMENT, Class.PrototypeEligible);
        manager = new LoanManager(IERC20(address(token)), vault, oracle, OPERATOR);

        vm.startPrank(OPERATOR);
        vault.setLoanManager(address(manager));
        oracle.setLoanManager(address(manager));
        oracle.pinRoute(FACILITY, Route.Development);
        oracle.setSigner(Route.Development, vm.addr(SIGNER_KEY), true);
        vm.stopPrank();

        account = new CreditAccount(address(manager), OWNER, AGENT, address(oracle));

        token.mint(LENDER, 50_000e6);
        vm.startPrank(LENDER);
        token.approve(address(vault), 50_000e6);
        vault.deposit(50_000e6);
        vm.stopPrank();
        vm.prank(OPERATOR);
        vault.activate();

        token.mint(address(account), 10_000e6);
        vm.warp(1_000_000);
    }

    function _originate(uint256 principal) internal returns (uint256) {
        Binding memory b =
            OracleHarness.binding(address(account), account.stateVersion(), FACILITY);
        // Build ONCE: signing a second, separately built assessment would
        // sign a different struct and the signature would not verify.
        Assessment memory a = OracleHarness.approvedQuote(
            b, ++seq, principal, 1_200, 10 days, 0, address(token),
            DEPLOYMENT, uint64(block.timestamp)
        );
        oracle.accept(a, OracleHarness.sign(oracle, a, SIGNER_KEY));
        return manager.originate(b, principal);
    }

    /// A token that reenters during repayment cannot repay twice.
    ///
    /// The second entry must find the loan already terminal. If ordering
    /// were interactions-before-effects it would find it Active and the
    /// vault would record the principal twice.
    function test_ReentrantRepaymentCannotDoubleSettle() public {
        uint256 loanId = _originate(5_000e6);
        vm.warp(block.timestamp + 1 days);

        uint256 outstandingBefore = vault.outstandingPrincipal();
        assertEq(outstandingBefore, 5_000e6, "one loan is outstanding");

        token.arm(manager, loanId);
        manager.repay(loanId);

        assertEq(token.reentryAttempts(), 1, "the attack must actually fire");
        assertGt(token.lastRevert().length, 0, "and the reentry must have reverted");

        // The ledger reflects exactly one repayment.
        assertEq(vault.outstandingPrincipal(), 0, "principal returns once");
        assertEq(
            uint8(manager.status(loanId)),
            uint8(LoanManager.Status.Repaid),
            "and the loan is terminal"
        );
    }

    /// The reentry fails for the RIGHT reason: the loan is already
    /// terminal when the callback lands, which is what
    /// checks-effects-interactions guarantees.
    function test_ReentryFindsTheLoanAlreadyTerminal() public {
        uint256 loanId = _originate(5_000e6);
        vm.warp(block.timestamp + 1 days);

        token.arm(manager, loanId);
        manager.repay(loanId);

        assertEq(
            token.lastRevert(),
            abi.encodeWithSelector(
                LoanManager.WrongStatus.selector,
                LoanManager.Status.Active,
                LoanManager.Status.Repaid
            ),
            "the callback must see a repaid loan, not an active one"
        );
    }

    /// A token that reenters during COLLECTION cannot collect twice
    /// against the same outstanding principal.
    ///
    /// Recovery pulls from the account exactly as repayment does, so it
    /// carries the same exposure: the callback must not observe principal
    /// that the outer call has already accounted for.
    function test_ReentrantCollectionCannotDoubleCount() public {
        uint256 loanId = _originate(5_000e6);
        vm.warp(block.timestamp + 10 days);

        Binding memory b =
            OracleHarness.binding(address(account), account.stateVersion(), FACILITY);
        vm.prank(OPERATOR);
        manager.openRecovery(loanId, OPERATOR, b);

        uint256 owed = manager.debtDue(loanId);
        token.armCollect(manager, loanId, owed);

        vm.prank(OPERATOR);
        manager.collect(loanId, owed);

        assertEq(token.reentryAttempts(), 1, "the attack must actually fire");

        // Principal came back exactly once.
        assertEq(vault.outstandingPrincipal(), 0, "principal returns once");
        assertEq(vault.realizedLoss(), 0, "and nothing was written off");
    }
}
