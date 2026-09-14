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
	"encoding/json"
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
	// `docs/09` calls beat 6 "the single most convincing moment" -- a
	// reverted withdrawal, demonstrated rather than claimed. It ran only
	// in a terminal until 14 September; emitting the results lets the
	// interface render the SAME on-chain evidence instead of retelling it.
	jsonOut := flag.String("json", "",
		"also write the checks to this file, for the web bundle")
	flag.Parse()

	checks, err := run(*rpcURL, *manager, *account, *settlement, *owner, *vault, *risky)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, checks); err != nil {
			fmt.Fprintf(os.Stderr, "verify: %v\n", err)
			os.Exit(1)
		}
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

	// Beat "lender": the epoch's ledger, read from the vault.
	//
	// `docs/09` asks what cash is available, what principal is exposed,
	// and what was realized. These are recorded rather than only printed
	// so the interface can show the LENDER's side from chain state --
	// otherwise the demo only ever tells the borrower's story.
	//
	// Reported as separate ledgers on purpose: `docs/05` keeps idle cash,
	// outstanding principal, collected interest and realized loss apart,
	// and uncollected interest is never withdrawable cash.
	for _, ledger := range []struct{ call, name string }{
		{"idleCash()", "vault idle cash"},
		{"outstandingPrincipal()", "vault principal outstanding"},
		{"realizedLoss()", "vault realized loss"},
	} {
		v, err := callUint(ctx, rpc, vault, ledger.call, block.Number)
		if err != nil {
			continue
		}
		checks = append(checks, check{
			beat: "lender", name: ledger.name,
			// A ledger read is evidence, not a pass/fail assertion: zero
			// realized loss is the correct value for a clean run, and
			// marking it failed would be a lie about a healthy epoch.
			passed: true,
			detail: fmt.Sprintf("%s base units", v),
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
//
// Nodes disagree about where the revert data goes. Anvil writes it into
// the message ("custom error 0xe9d4b4e2"); Monad returns a separate
// `data:` field and additionally renders the bytes as text, so the
// message reads `execution reverted: r<mojibake>3` with no usable hex.
// Prefer the data field, which is the only place the selector is
// reliably machine-readable, and fall back to scanning the message.
func errorSelector(err error) string {
	if err == nil {
		return "none"
	}
	msg := err.Error()
	if sel := hexAfter(msg, "data: \""); sel != "" {
		return sel
	}
	if sel := hexAfter(msg, "custom error "); sel != "" {
		return sel
	}
	// Last resort. Deliberately not the first "0x" in the message: an
	// address in the text would be mistaken for a selector and compared
	// against the expected one, reporting a confident wrong answer.
	return "unknown"
}

// hexAfter reads a 4-byte hex selector immediately following marker.
func hexAfter(msg, marker string) string {
	i := strings.Index(msg, marker)
	if i < 0 {
		return ""
	}
	rest := msg[i+len(marker):]
	if !strings.HasPrefix(rest, "0x") || len(rest) < 10 {
		return ""
	}
	sel := rest[:10]
	for _, c := range sel[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return ""
		}
	}
	return strings.ToLower(sel)
}

func revertDetail(err error) string {
	if err == nil {
		return "no revert"
	}
	msg := err.Error()
	// Report the selector rather than the node's rendering of it: Monad
	// decodes the revert bytes as text, so the raw message is mojibake
	// that says nothing about which error fired.
	if sel := errorSelector(err); sel != "unknown" && sel != "none" {
		return "custom error " + sel
	}
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

// writeJSON records the checks so the interface can show what actually
// happened on chain.
//
// The detail string is carried verbatim -- it holds the revert selector,
// and `docs/09` beat 6 turns on the two refusals being DIFFERENT:
// `0xe9d4b4e2` (debt is open) versus `0x72cb8533` (not the owner). A
// screen that collapsed them into "reverted" would lose the point.
func writeJSON(path string, checks []check) error {
	type row struct {
		Beat   string `json:"beat"`
		Name   string `json:"name"`
		Passed bool   `json:"passed"`
		Detail string `json:"detail"`
	}
	out := make([]row, 0, len(checks))
	for _, c := range checks {
		out = append(out, row{
			Beat: c.beat, Name: c.name, Passed: c.passed, Detail: c.detail,
		})
	}
	body, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return fmt.Errorf("encode checks: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}
