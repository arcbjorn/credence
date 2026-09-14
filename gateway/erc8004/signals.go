package erc8004

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// Tag vocabulary. Namespaced so a consumer filtering `readAllFeedback`
// selects Credence's entries rather than colliding with another client's
// meaning for a bare word like "outcome". The live registry already holds
// entries tagged "quality" and "crosscheck" from other clients.
const (
	TagOutcome  = "credence.outcome"
	TagInterest = "credence.interest"
	TagLoss     = "credence.loss"
	TagHeld     = "credence.held"

	OutcomeRepaid     = "repaid"
	OutcomeRecovered  = "recovered"
	OutcomeDefaulted  = "defaulted"
	settlementDecimal = 6

	// loanTagPrefix namespaces the loan id held in tag2.
	//
	// The id has to live in a field a READER can see. feedbackURI and
	// feedbackHash are write-only in practice: neither readFeedback nor
	// readAllFeedback returns them, and they survive only in the
	// NewFeedback event, which a public RPC will not scan cheaply. Tag2
	// is returned by both views and is filterable, so the id goes there
	// and idempotency stays a single call rather than a log crawl.
	loanTagPrefix = "loan:"
)

// DeploymentTag is a short digest of the chain and loan manager.
//
// Loan ids restart at 1 for every deployment, so a bare `loan:1` from one
// manager is indistinguishable from another's -- and because this tag is
// what idempotency keys on, the collision does not merely confuse a
// reader, it makes the second deployment's loan 1 permanently
// unpublishable. That is the same defect `docs/06` records against the
// old keccak("loan-N") evidence reference, so it must not reappear here.
//
// sha256 rather than keccak: the independent reader is stdlib-only by
// design, and Python has sha256 but not keccak. A digest a reader cannot
// recompute would make the tag unverifiable.
func DeploymentTag(chainID uint64, manager string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s",
		chainID, strings.ToLower(manager))))
	return hex.EncodeToString(sum[:4])
}

// LoanTag is the tag2 value identifying one obligation.
func LoanTag(chainID uint64, manager string, loanID uint64) string {
	return fmt.Sprintf("%s%s:%d",
		loanTagPrefix, DeploymentTag(chainID, manager), loanID)
}

// ParseLoanTag recovers a loan id from tag2, reporting false for a tag
// written by a different deployment or a different client.
//
// Strict about the deployment on purpose: treating another manager's
// `loan:1` as ours would mark an obligation published that was never
// reported, and the outcome would silently never reach the registry.
func ParseLoanTag(tag2 string, chainID uint64, manager string) (uint64, bool) {
	want := loanTagPrefix + DeploymentTag(chainID, manager) + ":"
	if len(tag2) <= len(want) || tag2[:len(want)] != want {
		return 0, false
	}
	return parseUint(tag2[len(want):])
}

// Outcome classes, matching the loan manager's terminal states.
const (
	Repaid uint8 = iota
	Recovered
	Defaulted
)

// LoanOutcome is one resolved obligation, as the loan manager recorded it.
type LoanOutcome struct {
	AgentID       *big.Int
	LoanID        *big.Int
	Outcome       uint8
	Principal     *big.Int
	InterestPaid  *big.Int
	PrincipalLoss *big.Int
	HeldSeconds   uint64
	// EvidenceURI locates the retained record, and EvidenceHash commits
	// to its bytes. An entry without both asserts a number a reader
	// cannot check.
	EvidenceURI  string
	EvidenceHash [32]byte
	// ChainID and Manager scope the loan id: ids restart at 1 per
	// deployment, so the tag must say which one.
	ChainID uint64
	Manager string
}

// Signals expands one resolved loan into the registry entries that
// describe it.
//
// **One entry per signal, not one per loan.** The registry stores a
// single int128 per entry, and `docs/06` forbids combining a repayment, a
// realised loss, interest earned and duration held into one fact:
// "A repayment, a profitable strategy, a reliable forecast, and absence
// of default are separate signals. Do not combine them into one."
//
// A composite score would fit the field and lose exactly what the
// document protects, so each signal is published separately and tagged.
// A consumer wanting only defaults filters tag1 and never has to unpack a
// number whose construction it would have to trust.
//
// Every entry carries the same feedbackURI and hash, so the four rows
// resolve to one retained record rather than four disconnected claims --
// and the same tag2 (`loan:N`), which is how a reader groups them and how
// this client recognises what it has already published.
func Signals(o LoanOutcome) ([]Feedback, error) {
	if o.AgentID == nil || o.LoanID == nil {
		return nil, fmt.Errorf("agent id and loan id required")
	}
	if o.Principal == nil || o.InterestPaid == nil || o.PrincipalLoss == nil {
		return nil, fmt.Errorf("loan %s: amounts required", o.LoanID)
	}
	outcome, err := outcomeTag(o.Outcome)
	if err != nil {
		return nil, err
	}

	base := Feedback{
		AgentID:      o.AgentID,
		FeedbackURI:  o.EvidenceURI,
		FeedbackHash: o.EvidenceHash,
	}

	// The outcome class. Value is the principal, so a reader sees the
	// size the class applies to; the class itself rides in tag1, where it
	// stays categorical rather than becoming a number on a scale nobody
	// defined, and stays filterable -- `credence.outcome.defaulted`
	// selects every default in one call.
	out := base
	out.Tag1 = TagOutcome + "." + outcome
	out.Tag2 = LoanTag(o.ChainID, o.Manager, o.LoanID.Uint64())
	out.Value = new(big.Int).Set(o.Principal)
	out.ValueDecimals = settlementDecimal

	// Interest actually collected. Separate from the outcome because a
	// loan can repay in full and earn little, or recover and still pay
	// interest -- collapsing them would hide which happened.
	interest := base
	interest.Tag1 = TagInterest
	interest.Tag2 = LoanTag(o.ChainID, o.Manager, o.LoanID.Uint64())
	interest.Value = new(big.Int).Set(o.InterestPaid)
	interest.ValueDecimals = settlementDecimal

	// Realised principal loss, published as a NEGATIVE value. The
	// registry's value is signed precisely so a loss is not reported as
	// though it were a gain, and a consumer summing this tag gets a
	// figure that moves the right way.
	loss := base
	loss.Tag1 = TagLoss
	loss.Tag2 = LoanTag(o.ChainID, o.Manager, o.LoanID.Uint64())
	loss.Value = new(big.Int).Neg(o.PrincipalLoss)
	loss.ValueDecimals = settlementDecimal

	// Duration held, in seconds. `docs/06` weights real elapsed exposure
	// and gives same-block borrow/repay zero weight, so a reader
	// recomputing that weight needs the duration as its own fact rather
	// than inferring it from timestamps it cannot see.
	held := base
	held.Tag1 = TagHeld
	held.Tag2 = LoanTag(o.ChainID, o.Manager, o.LoanID.Uint64())
	held.Value = new(big.Int).SetUint64(o.HeldSeconds)
	held.ValueDecimals = 0

	entries := []Feedback{out, interest, loss, held}
	for i := range entries {
		if err := checkInt128(entries[i].Value); err != nil {
			return nil, fmt.Errorf("loan %s %s: %w",
				o.LoanID, entries[i].Tag1, err)
		}
	}
	return entries, nil
}

func outcomeTag(o uint8) (string, error) {
	switch o {
	case Repaid:
		return OutcomeRepaid, nil
	case Recovered:
		return OutcomeRecovered, nil
	case Defaulted:
		return OutcomeDefaulted, nil
	default:
		// An unrecognised class must not publish as though it were a
		// known one: a reader filtering on tag2 would silently miscount.
		return "", fmt.Errorf("unknown outcome class %d", o)
	}
}
