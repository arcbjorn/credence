// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {CreditAccount} from "../src/CreditAccount.sol";
import {StubOracle} from "./CreditAccount.t.sol";
import {FixedPriceSource} from "../src/FixedPriceSource.sol";
import {IERC20} from "../src/IERC20.sol";
import {IPriceSource} from "../src/IPriceSource.sol";
import {SpotVenue} from "../src/SpotVenue.sol";
import {StrategyAdapter} from "../src/StrategyAdapter.sol";
import {TestRiskyAsset} from "../src/TestRiskyAsset.sol";
import {TestSettlementToken} from "../src/TestSettlementToken.sol";

contract StrategyAdapterTest is Test {
    TestSettlementToken settlement;
    TestRiskyAsset risky;
    SpotVenue venue;
    FixedPriceSource priceSource;
    StrategyAdapter adapter;
    CreditAccount account;

    address constant OPERATOR = address(0xA1);
    address constant OWNER = address(0xB2);
    address constant AGENT = address(0xC3);
    address constant LOAN_MANAGER = address(0xD4);

    uint256 constant PRICE = 2_000e6; // 2000 settlement per risky unit
    uint32 constant PRICE_SCALE = 6;
    uint32 constant RISKY_DECIMALS = 18;

    function setUp() public {
        settlement = new TestSettlementToken();
        risky = new TestRiskyAsset();

        venue = new SpotVenue(
            IERC20(address(settlement)),
            IERC20(address(risky)),
            PRICE,
            PRICE_SCALE,
            RISKY_DECIMALS,
            OPERATOR
        );
        priceSource = new FixedPriceSource(PRICE, PRICE_SCALE, OPERATOR);

        adapter = new StrategyAdapter(
            IERC20(address(settlement)),
            IERC20(address(risky)),
            venue,
            IPriceSource(address(priceSource)),
            200, // 2% tolerance
            3_600, // one hour
            RISKY_DECIMALS
        );

        account = new CreditAccount(LOAN_MANAGER, OWNER, AGENT, address(new StubOracle()));
        vm.prank(LOAN_MANAGER);
        account.setAdapter(address(adapter), true);

        // Liquidity on both sides of the venue.
        settlement.mint(address(venue), 1_000_000e6);
        risky.mint(address(venue), 1_000e18);
        settlement.mint(address(account), 10_000e6);
    }

    function _approveAdapter(address token, uint256 amount) private {
        // Approval goes through the account's own gated path, which only
        // permits whitelisted adapters as spenders.
        vm.prank(AGENT);
        account.approveAdapter(token, address(adapter), amount);
    }

    // --- The core capability ---

    /// Principal can be deployed: settlement becomes the risky asset, and
    /// the output returns to the account rather than to the adapter.
    function test_acquireReturnsOutputToTheAccount() public {
        _approveAdapter(address(settlement), 4_000e6);

        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));

        assertEq(settlement.balanceOf(address(account)), 6_000e6, "settlement spent");
        assertEq(risky.balanceOf(address(account)), 2e18, "risky received by the account");
        assertEq(risky.balanceOf(address(adapter)), 0, "adapter holds nothing");
        assertEq(settlement.balanceOf(address(adapter)), 0, "adapter holds nothing");
    }

    /// And it can be unwound back to settlement.
    function test_unwindReturnsSettlement() public {
        _approveAdapter(address(settlement), 4_000e6);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));

        _approveAdapter(address(risky), 2e18);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.unwind, (2e18)));

        assertEq(risky.balanceOf(address(account)), 0);
        assertEq(settlement.balanceOf(address(account)), 10_000e6, "round trip returns value");
    }

    // --- The guard docs/05 requires be tested explicitly ---

    /// **A whitelisted venue is not sufficient.** A venue quoting a bad
    /// price transfers value to a cooperating counterparty, and the
    /// adapter must reject it against the independent reference.
    function test_badPriceTradeIsRejected() public {
        // The venue now pays half the reference price.
        vm.prank(OPERATOR);
        venue.setPrice(4_000e6);

        _approveAdapter(address(settlement), 4_000e6);

        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.AdapterCallFailed.selector);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));

        // Nothing moved: the account keeps its settlement.
        assertEq(settlement.balanceOf(address(account)), 10_000e6);

        // And it failed for the RIGHT reason. `execute` wraps every
        // adapter revert as AdapterCallFailed, so that selector alone
        // would also pass if the trade broke for an unrelated cause.
        // Called directly, the adapter names the slippage guard.
        settlement.mint(address(this), 4_000e6);
        settlement.approve(address(adapter), 4_000e6);
        vm.expectRevert(
            abi.encodeWithSelector(
                StrategyAdapter.SlippageExceeded.selector, uint256(2e18), uint256(1e18), uint32(200)
            )
        );
        adapter.acquire(4_000e6);
    }

    /// A favourable fill is not an error. Refusing it would block a better
    /// price for no benefit to the account.
    function test_favourablePriceIsAccepted() public {
        vm.prank(OPERATOR);
        venue.setPrice(1_000e6); // half price: the account gets more risky

        _approveAdapter(address(settlement), 4_000e6);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));

        assertEq(risky.balanceOf(address(account)), 4e18, "a better fill is kept");
    }

    /// Within tolerance is allowed, so ordinary market movement does not
    /// block legitimate execution.
    function test_smallDeviationIsAllowed() public {
        // 1% worse than reference, inside the 2% tolerance.
        vm.prank(OPERATOR);
        venue.setPrice(2_020e6);

        _approveAdapter(address(settlement), 4_000e6);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));

        assertGt(risky.balanceOf(address(account)), 0);
    }

    /// A stale reference cannot detect a bad fill, so trading against one
    /// is refused rather than waved through.
    function test_stalePriceBlocksExecution() public {
        vm.warp(block.timestamp + 2 hours);

        _approveAdapter(address(settlement), 4_000e6);
        vm.prank(AGENT);
        vm.expectRevert(CreditAccount.AdapterCallFailed.selector);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));
    }

    // --- Custody ---

    /// The adapter leaves no standing allowance for a later call to drain.
    function test_noStandingAllowanceSurvives() public {
        _approveAdapter(address(settlement), 4_000e6);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));

        assertEq(
            settlement.allowance(address(adapter), address(venue)),
            0,
            "approval must be cleared after the trade"
        );
    }

    /// Execution advances the account's state version, invalidating any
    /// assessment taken against the previous portfolio.
    function test_executionAdvancesStateVersion() public {
        uint64 before = account.stateVersion();
        _approveAdapter(address(settlement), 4_000e6);
        vm.prank(AGENT);
        account.execute(address(adapter), abi.encodeCall(StrategyAdapter.acquire, (4_000e6)));

        assertGt(account.stateVersion(), before);
    }

    /// An unregistered adapter cannot be called, whatever it claims to do.
    function test_unregisteredAdapterIsRejected() public {
        StrategyAdapter rogue = new StrategyAdapter(
            IERC20(address(settlement)),
            IERC20(address(risky)),
            venue,
            IPriceSource(address(priceSource)),
            200,
            3_600,
            RISKY_DECIMALS
        );

        vm.prank(AGENT);
        vm.expectRevert(
            abi.encodeWithSelector(CreditAccount.AdapterNotAllowed.selector, address(rogue))
        );
        account.execute(address(rogue), abi.encodeCall(StrategyAdapter.acquire, (1e6)));
    }
}

/// Approval is a new privileged path, so it needs the same scrutiny as
/// withdrawal: it must not become a route for value to leave the account.
contract AdapterApprovalTest is Test {
    TestSettlementToken settlement;
    CreditAccount account;

    address constant OWNER = address(0xB2);
    address constant AGENT = address(0xC3);
    address constant LOAN_MANAGER = address(0xD4);
    address constant ADAPTER = address(0xE5);
    address constant ATTACKER = address(0xBAD);

    function setUp() public {
        settlement = new TestSettlementToken();
        account = new CreditAccount(LOAN_MANAGER, OWNER, AGENT, address(new StubOracle()));
        settlement.mint(address(account), 10_000e6);
        vm.prank(LOAN_MANAGER);
        account.setAdapter(ADAPTER, true);
    }

    /// An unregistered spender cannot be approved, so approval cannot be
    /// used to hand an arbitrary address a claim on the account.
    function test_cannotApproveUnregisteredSpender() public {
        vm.prank(AGENT);
        vm.expectRevert(abi.encodeWithSelector(CreditAccount.AdapterNotAllowed.selector, ATTACKER));
        account.approveAdapter(address(settlement), ATTACKER, type(uint256).max);
    }

    /// And an outsider cannot grant approvals at all.
    function test_outsiderCannotApprove() public {
        vm.prank(ATTACKER);
        vm.expectRevert(CreditAccount.NotOwnerOrAgent.selector);
        account.approveAdapter(address(settlement), ADAPTER, 1e6);
    }

    /// A revoked adapter keeps its allowance until cleared, so clearing
    /// must remain possible after revocation. Otherwise a revocation would
    /// strand a live claim on the account permanently.
    function test_revokedAdapterAllowanceCanStillBeCleared() public {
        vm.prank(AGENT);
        account.approveAdapter(address(settlement), ADAPTER, 5_000e6);

        vm.prank(LOAN_MANAGER);
        account.setAdapter(ADAPTER, false);
        assertEq(
            settlement.allowance(address(account), ADAPTER),
            5_000e6,
            "revocation alone does not clear the allowance"
        );

        vm.prank(AGENT);
        account.approveAdapter(address(settlement), ADAPTER, 0);
        assertEq(
            settlement.allowance(address(account), ADAPTER),
            0,
            "zeroing must work even after revocation"
        );
    }

    /// But a revoked adapter cannot be granted a NEW allowance.
    function test_revokedAdapterCannotBeReapproved() public {
        vm.prank(LOAN_MANAGER);
        account.setAdapter(ADAPTER, false);

        vm.prank(AGENT);
        vm.expectRevert(abi.encodeWithSelector(CreditAccount.AdapterNotAllowed.selector, ADAPTER));
        account.approveAdapter(address(settlement), ADAPTER, 1e6);
    }
}
