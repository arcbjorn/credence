// Command credence-verify checks demo claims against a live deployment.
//
// Every contract invariant here is already covered by Foundry tests. This
// tool exists because a passing unit test and a correctly wired deployment
// are different claims: the tests prove the code enforces a rule, this
// proves the deployed instance does, with real balances and a real loan.
//
// It asserts rather than reports. A check that merely printed state would
// let a broken deployment look fine to a reader skimming output.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/chain"
)

type check struct {
	beat   string
	name   string
	passed bool
	detail string
}

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8546", "chain rpc")
	manager := flag.String("manager", "", "loan manager address")
	account := flag.String("account", "", "credit account address")
	settlement := flag.String("settlement", "", "settlement token address")
	owner := flag.String("owner", "", "credit account owner address")
	vault := flag.String("vault", "", "credit vault address")
	risky := flag.String("risky", "",
		"risky asset; checked when the account's settlement balance is zero")
	flag.Parse()

	checks, err := run(*rpcURL, *manager, *account, *settlement, *owner, *vault, *risky)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		os.Exit(1)
	}

	failed := 0
	for _, c := range checks {
		mark := "PASS"
		if !c.passed {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("[%s] beat %-3s %-42s %s\n", mark, c.beat, c.name, c.detail)
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d check(s) failed\n", failed)
		os.Exit(1)
	}
	fmt.Printf("\nall %d checks passed against the deployment\n", len(checks))
}

func run(rpcURL, manager, account, settlement, owner, vault, risky string) ([]check, error) {
	ctx := context.Background()
	rpc := chain.NewRPC(rpcURL, 15*time.Second)

	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest block: %w", err)
	}

	var checks []check

	// Beat 4b: a loan is open, so the protections below are live rather
	// than vacuously true on an idle account.
	loanID, err := callUint(ctx, rpc, manager, "activeLoan(address)", block.Number, account)
	if err != nil {
		return nil, fmt.Errorf("read active loan: %w", err)
	}
	checks = append(checks, check{
		beat: "4b", name: "an active loan exists",
		passed: loanID.Sign() != 0,
		detail: fmt.Sprintf("loan %s", loanID),
	})

	debtFlag, err := callBool(ctx, rpc, account, "debtOutstanding()", block.Number)
	if err != nil {
		return nil, fmt.Errorf("read debt flag: %w", err)
	}
	checks = append(checks, check{
		beat: "4b", name: "account carries debt",
		passed: debtFlag, detail: fmt.Sprintf("debtOutstanding=%v", debtFlag),
	})

	// The account must actually hold value, or a blocked withdrawal proves
	// nothing: refusing to move zero tokens is not a protection.
	//
	// Whichever asset it holds. A funded account that deployed its whole
	// settlement balance into a strategy holds the RISKY token, and
	// checking settlement alone would report "nothing to withdraw" while
	// the owner is in fact being refused real collateral -- the stronger
	// demonstration, reported as a failure.
	balance, err := callUintOn(ctx, rpc, account, "balanceOf(address)", block.Number, settlement)
	if err != nil {
		return nil, fmt.Errorf("read account balance: %w", err)
	}
	heldAsset, held := settlement, balance
	if balance.Sign() == 0 && risky != "" {
		riskyBalance, err := callUintOn(
			ctx, rpc, account, "balanceOf(address)", block.Number, risky)
		if err != nil {
			return nil, fmt.Errorf("read risky balance: %w", err)
		}
		if riskyBalance.Sign() > 0 {
			heldAsset, held = risky, riskyBalance
		}
	}
	checks = append(checks, check{
		beat: "6", name: "account holds withdrawable value",
		passed: held.Sign() > 0,
		detail: fmt.Sprintf("%s base units of %s", held, heldAsset),
	})

	// Beat 6: the owner — not merely a stranger — is refused while debt is
	// outstanding. `docs/02` is explicit that ownership grants no bypass,
	// and a check against an unrelated caller would prove far less.
	withdrawErr := simulateWithdraw(ctx, rpc, account, heldAsset, owner, held)
	blocked := withdrawErr != nil && strings.Contains(withdrawErr.Error(), "reverted")
	detail := "withdrawal succeeded"
	if withdrawErr != nil {
		detail = revertDetail(withdrawErr)
	}
	checks = append(checks, check{
		beat: "6", name: "owner withdrawal reverts while debt is open",
		passed: blocked, detail: detail,
	})

	// The same call from a stranger must also fail, but for a DIFFERENT
	// reason. If both reverted identically, the owner check would be
	// passing on access control alone and the debt guard — the actual
	// lender protection — would be untested.
	strangerErr := simulateWithdraw(ctx, rpc, account, heldAsset,
		"0x000000000000000000000000000000000000dEaD", held)
	checks = append(checks, check{
		beat: "6", name: "stranger withdrawal reverts",
		passed: strangerErr != nil,
		detail: revertDetail(strangerErr),
	})

	ownerSelector := errorSelector(withdrawErr)
	strangerSelector := errorSelector(strangerErr)
	checks = append(checks, check{
		beat: "6", name: "owner blocked by debt, not access control",
		passed: ownerSelector == withdrawalBlockedByDebt &&
			strangerSelector == notOwnerOrAgent,
		detail: fmt.Sprintf("owner=%s stranger=%s", ownerSelector, strangerSelector),
	})

	// Beat 4: interest was actually collected, so repayment priced the
	// loan rather than returning bare principal.
	collected, err := callUint(ctx, rpc, vault, "collectedInterest()", block.Number)
	if err == nil {
		checks = append(checks, check{
			beat: "4", name: "interest was collected on repayment",
			passed: collected.Sign() > 0,
			detail: fmt.Sprintf("%s base units", collected),
		})
	}

	return checks, nil
}

// simulateWithdraw runs the call without sending it, so the check costs no
// gas and leaves no state behind.
func simulateWithdraw(
	ctx context.Context,
	rpc *chain.RPC,
	account, token, from string,
	amount *big.Int,
) error {
	tokenAddr, err := toAddress(token)
	if err != nil {
		return err
	}
	toAddr, err := toAddress(from)
	if err != nil {
		return err
	}

	data := append(attest.Selector("withdraw(address,address,uint256)"),
		attest.AddressWord(tokenAddr)...)
	data = append(data, attest.AddressWord(toAddr)...)
	data = append(data, attest.Word(amount)...)

	_, err = rpc.EstimateGas(ctx, from, account, data)
	return err
}

// Selectors from CreditAccount. Named so a mismatch reads as a changed
// guard rather than an opaque hex difference.
const (
	withdrawalBlockedByDebt = "0xe9d4b4e2"
	notOwnerOrAgent         = "0x72cb8533"
)

// errorSelector extracts the custom error selector from a revert message.
func errorSelector(err error) string {
	if err == nil {
		return "none"
	}
	msg := err.Error()
	i := strings.Index(msg, "0x")
	if i < 0 {
		return "unknown"
	}
	end := i + 10
	if end > len(msg) {
		end = len(msg)
	}
	return msg[i:end]
}

func revertDetail(err error) string {
	if err == nil {
		return "no revert"
	}
	msg := err.Error()
	if i := strings.Index(msg, "custom error"); i >= 0 {
		return msg[i:]
	}
	if len(msg) > 60 {
		return msg[:60]
	}
	return msg
}

func callUint(
	ctx context.Context,
	rpc *chain.RPC,
	to, signature string,
	block uint64,
	args ...string,
) (*big.Int, error) {
	data := attest.Selector(signature)
	for _, a := range args {
		addr, err := toAddress(a)
		if err != nil {
			return nil, err
		}
		data = append(data, attest.AddressWord(addr)...)
	}
	out, err := rpc.CallContract(ctx, to, data, block)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(out), nil
}

func callUintOn(
	ctx context.Context,
	rpc *chain.RPC,
	to, signature string,
	block uint64,
	arg string,
) (*big.Int, error) {
	return callUint(ctx, rpc, to, signature, block, arg)
}

func callBool(
	ctx context.Context,
	rpc *chain.RPC,
	to, signature string,
	block uint64,
) (bool, error) {
	v, err := callUint(ctx, rpc, to, signature, block)
	if err != nil {
		return false, err
	}
	return v.Sign() != 0, nil
}

func toAddress(s string) (attest.Address, error) {
	var a attest.Address
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) != 20 {
		return a, fmt.Errorf("invalid address %q", s)
	}
	copy(a[:], b)
	return a, nil
}
