// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Script} from "forge-std/Script.sol";
import {console2} from "forge-std/console2.sol";

import {CreditAccount} from "../src/CreditAccount.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {TestRiskyAsset} from "../src/TestRiskyAsset.sol";
import {TestSettlementToken} from "../src/TestSettlementToken.sol";

/// @title AddAccount
/// @notice Adds one more credit account to a facility that is already live.
///
/// `Scenario.s.sol` funds the vault and calls `activate()`, which requires
/// `Phase.Funding` -- so running it a second time to create a second agent
/// reverts, and the demos could only ever show ONE borrower. That is why
/// the globe had a single point for an agent with real chain history.
///
/// This does the part of `Scenario` that is per-agent and nothing else: a
/// new `CreditAccount`, its own equity, and its execution route. The vault
/// is left alone, so it can be called once per agent against a facility
/// that is already active.
///
/// Like `Scenario`, it stops short of origination. A loan needs an
/// assessment the Rust engine produces and the Go gateway signs; minting
/// one from Solidity would bypass the authorization path the demos exist
/// to show.
contract AddAccount is Script {
    function run() external {
        uint256 deployerKey = vm.envUint("DEPLOYER_KEY");
        address operator = vm.addr(deployerKey);
        address agent = vm.envAddress("AGENT_ADDRESS");

        address tokenAddr = vm.envAddress("SETTLEMENT_TOKEN");
        address managerAddr = vm.envAddress("LOAN_MANAGER_ADDRESS");

        uint256 agentEquity = vm.envOr("AGENT_EQUITY", uint256(10_000e6));
        uint256 riskyEquity = vm.envOr("AGENT_RISKY_EQUITY", uint256(0));
        address riskyAsset = vm.envOr("RISKY_ASSET", address(0));
        address adapter = vm.envOr("STRATEGY_ADAPTER", address(0));

        TestSettlementToken token = TestSettlementToken(tokenAddr);

        vm.startBroadcast(deployerKey);

        // The oracle comes from the manager rather than a second env var,
        // so the account can never be bound to a different one than the
        // manager consults.
        CreditAccount account = new CreditAccount(
            managerAddr, operator, agent, address(LoanManager(managerAddr).oracle())
        );
        token.mint(address(account), agentEquity);
        if (riskyEquity > 0 && riskyAsset != address(0)) {
            TestRiskyAsset(riskyAsset).mint(address(account), riskyEquity);
        }

        // Register the one supported execution route. Without this the
        // account can hold principal but never deploy it, which is the
        // difference between a loan and a deposit.
        if (adapter != address(0)) {
            LoanManager(managerAddr).setAccountAdapter(account, adapter, true);
            // Closing a position reduces risk, so it needs no standing
            // permission: an account must always be able to exit.
            LoanManager(managerAddr)
                .setAccountDeRiskingAction(
                    account, adapter, bytes4(keccak256("unwind(uint256)")), true
                );
        }

        vm.stopBroadcast();

        // Same phrasing as `Scenario`, so one grep finds an account
        // whichever script created it.
        console2.log("credit account", address(account));
        console2.log("agent equity  ", agentEquity);
        console2.log("agent risky   ", riskyEquity);
    }
}
