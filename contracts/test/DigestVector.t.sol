// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import {Test} from "forge-std/Test.sol";
import {console2} from "forge-std/console2.sol";
import {RiskOracle} from "../src/RiskOracle.sol";
import {OracleHarness} from "./OracleHarness.sol";
import {Assessment, Binding, Class, Decision, Kind, Permission, Route, Terms} from "../src/Assessment.sol";

/// Emits the digest Solidity computes for a fixed assessment, so the Go
/// signer can be pinned against it. If the two encoders ever diverge, the
/// oracle would recover an unexpected signer and reject a valid assessment —
/// a failure that looks like a key problem but is an encoding one.
contract DigestVectorTest is Test {
    function test_emitMonitoringDigest() public {
        RiskOracle oracle = new RiskOracle(address(0xA1), 7, Class.PrototypeEligible);

        Binding memory b = OracleHarness.binding(address(0xACC7), 3, 1);
        Assessment memory a = OracleHarness.approvedQuote(
            b, 5, 0, 0, 0, 0, address(0x05D0), 7, 1000
        );
        // Shape it as a monitoring result: no request, no grant.
        a.kind = Kind.Monitoring;
        a.decision = Decision.Approved;
        a.permission = Permission.Allowed;
        a.requestId = bytes32(0);
        a.intentHash = bytes32(0);
        a.requestedPrincipal = 0;
        a.terms = Terms(0, 0, 0, 0, bytes32(0));
        a.loanId = 42;
        a.validAfter = 1000;
        a.validUntil = 4600;

        console2.log("domain_separator");
        console2.logBytes32(oracle.domainSeparator());
        console2.log("struct_hash");
        console2.logBytes32(OracleHarness.structHash(a));
        // The oracle keys its state on this, which is NOT the gateway's own
        // binding identifier. Emitting it pins the Go derivation.
        console2.log("chain_key");
        console2.logBytes32(oracle.bindingKey(b));
    }
}

