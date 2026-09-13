// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Script} from "forge-std/Script.sol";
import {console2} from "forge-std/console2.sol";

import {CreditAccount} from "../src/CreditAccount.sol";
import {CreditVault} from "../src/CreditVault.sol";
import {LoanManager} from "../src/LoanManager.sol";
import {TestSettlementToken} from "../src/TestSettlementToken.sol";

/// @title Scenario
/// @notice Prepares a funded facility with a credit account ready to borrow.
///
/// Everything up to origination is done here, because it needs no signed
/// quote. Origination itself requires an assessment the Rust engine
/// produces and the Go gateway signs, so it deliberately stops short:
/// minting a loan from Solidity would bypass the authorization path the
/// demo exists to show.
contract Scenario is Script {
    function run() external {
        uint256 deployerKey = vm.envUint("DEPLOYER_KEY");
        address operator = vm.addr(deployerKey);
        address agent = vm.envAddress("AGENT_ADDRESS");

        address tokenAddr = vm.envAddress("SETTLEMENT_TOKEN");
        address vaultAddr = vm.envAddress("VAULT_ADDRESS");
        address managerAddr = vm.envAddress("LOAN_MANAGER_ADDRESS");

        uint256 lenderDeposit = vm.envOr("LENDER_DEPOSIT", uint256(50_000e6));
        uint256 agentEquity = vm.envOr("AGENT_EQUITY", uint256(5_000e6));

        TestSettlementToken token = TestSettlementToken(tokenAddr);
        CreditVault vault = CreditVault(vaultAddr);

        vm.startBroadcast(deployerKey);

        // Fund the vault. The operator stands in for a lender here; in a
        // real epoch these are distinct parties with a defined lockup.
        token.mint(operator, lenderDeposit);
        token.approve(vaultAddr, lenderDeposit);
        vault.deposit(lenderDeposit);
        vault.activate();

        // The agent's credit account, holding its own equity. `docs/02`:
        // an agent with no equity receives no credit in v0.
        CreditAccount account = new CreditAccount(managerAddr, operator, agent);
        token.mint(address(account), agentEquity);

        vm.stopBroadcast();

        console2.log("credit account", address(account));
        console2.log("agent equity  ", agentEquity);
        console2.log("vault idle    ", vault.idleCash());
        console2.log("vault phase   ", uint8(vault.phase()));
    }
}
