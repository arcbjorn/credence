package erc8004

import (
	"fmt"
	"math/big"
)

// Published reports which loans this client has already published for an
// agent, derived from the registry's own entries.
//
// The local adapter refused a duplicate on chain (`AlreadyPublished`).
// **ERC-8004 does not.** feedbackIndex auto-increments per
// (agentId, clientAddress), so sending the same outcome twice produces two
// entries and double-counts it — exactly what `docs/06` requires queued
// updates to avoid. The registry cannot be asked "have I published loan
// 3?", so the answer has to be reconstructed from what is already there.
//
// The loan id is carried in the feedbackURI rather than a tag, because
// tags are the signal vocabulary a reader filters on and overloading them
// with an identifier would make `credence.loss` mean two different things.
type Published map[uint64]bool

// LoanURI is the canonical evidence locator for one loan.
//
// It commits to the deployment as well as the loan: `docs/06` records that
// the old `keccak("loan-N")` reference was not evidence because the same
// loan number under a different manager produced the same value. Chain id
// and manager address make it specific, and the reader recomputes it
// rather than trusting a string it was handed.
func LoanURI(base string, chainID uint64, manager string, loanID uint64) string {
	if base == "" {
		return fmt.Sprintf("credence:%d:%s:loan:%d", chainID, manager, loanID)
	}
	return fmt.Sprintf("%s/%d/%s/loan/%d.json", base, chainID, manager, loanID)
}

// ParseLoanURI recovers the loan id from an evidence locator this client
// wrote, reporting false for anything it did not write.
//
// Deliberately strict about the deployment: a URI naming a different chain
// or manager describes a different obligation that happens to share a
// number, and treating it as published here would skip a loan that was
// never reported.
func ParseLoanURI(uri string, chainID uint64, manager string) (uint64, bool) {
	for _, form := range []string{
		fmt.Sprintf("credence:%d:%s:loan:", chainID, manager),
	} {
		if len(uri) > len(form) && uri[:len(form)] == form {
			return parseUint(uri[len(form):])
		}
	}
	// Hosted form: <base>/<chain>/<manager>/loan/<id>.json
	suffix := fmt.Sprintf("/%d/%s/loan/", chainID, manager)
	i := indexOf(uri, suffix)
	if i < 0 {
		return 0, false
	}
	rest := uri[i+len(suffix):]
	if len(rest) < 6 || rest[len(rest)-5:] != ".json" {
		return 0, false
	}
	return parseUint(rest[:len(rest)-5])
}

// PublishPlan is the calldata for one loan's signals, in order.
type PublishPlan struct {
	LoanID  uint64
	Entries []Feedback
}

// Plan expands resolved outcomes into the entries still to publish,
// skipping any loan already present in the registry for this client.
func Plan(
	outcomes []LoanOutcome,
	already Published,
) ([]PublishPlan, error) {
	var plans []PublishPlan
	for _, o := range outcomes {
		if o.LoanID == nil {
			return nil, fmt.Errorf("loan id required")
		}
		id := o.LoanID.Uint64()
		if already[id] {
			continue
		}
		entries, err := Signals(o)
		if err != nil {
			return nil, err
		}
		plans = append(plans, PublishPlan{LoanID: id, Entries: entries})
	}
	return plans, nil
}

func parseUint(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		d := uint64(c - '0')
		// Refuse rather than wrap: a wrapped id would mark the wrong
		// loan published and skip a real one.
		if v > (^uint64(0)-d)/10 {
			return 0, false
		}
		v = v*10 + d
	}
	return v, true
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// AgentID is a convenience for callers holding a uint64.
func AgentID(v uint64) *big.Int { return new(big.Int).SetUint64(v) }
