package main

import (
	"context"
	"fmt"
	"log"
	"math/big"

	"strings"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/erc8004"
	"github.com/arcbjorn/credence/gateway/wallet"
)

// readPublished asks the registry what this client has already said about
// the agents behind these loans.
//
// The registry offers no "have I published loan N?" query, so the answer
// is reconstructed from tag2, which every Credence entry sets to `loan:N`.
// The evidence URI cannot serve here: no view returns it, so keying on it
// would require scanning NewFeedback logs, which a public RPC will not do
// cheaply over any useful range.
//
// Reading filtered to THIS client matters: another client's entry about
// the same agent is not Credence's statement, and treating it as one
// would skip a loan that was never published.
func readPublished(
	ctx context.Context,
	rpc *chain.RPC,
	registry, client, manager string,
	chainID uint64,
	fromLoan, toLoan, blockNumber uint64,
) (erc8004.Published, error) {
	out := erc8004.Published{}

	// Agents differ per loan, so collect the distinct ones first rather
	// than assuming one agent owns the whole range.
	agents := map[uint64]bool{}
	for id := fromLoan; id <= toLoan; id++ {
		agent, err := loanAgent(ctx, rpc, manager, id, blockNumber)
		if err != nil {
			return nil, err
		}
		if agent != 0 {
			agents[agent] = true
		}
	}

	clientAddr, err := erc8004.ParseAddress(client)
	if err != nil {
		return nil, err
	}

	for agent := range agents {
		data, err := erc8004.EncodeReadAllFeedback(
			erc8004.AgentID(agent), []erc8004.Address{clientAddr}, "", "", true)
		if err != nil {
			return nil, err
		}
		raw, err := rpc.CallContract(ctx, registry, data, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("agent %d: %w", agent, err)
		}
		entries, err := erc8004.DecodeReadAllFeedback(raw)
		if err != nil {
			return nil, fmt.Errorf("agent %d: %w", agent, err)
		}
		for _, e := range entries {
			// A revoked entry still means the outcome was published.
			// Re-sending it would add a second live row beside the
			// revocation rather than correcting anything; `docs/06`
			// records a correction as a NEW version, deliberately.
			if id, ok := erc8004.ParseLoanTag(e.Tag2, chainID, manager); ok {
				out[id] = true
			}
		}
	}
	return out, nil
}

// publishStandard sends one loan's signals to the ERC-8004 registry.
//
// Returns the number of entries written, zero when the loan was already
// published. Each signal is its own transaction because the registry
// takes one value per call; they are sent in order so a partial failure
// leaves a prefix rather than an arbitrary subset.
func publishStandard(
	ctx context.Context,
	rpc *chain.RPC,
	w *wallet.Wallet,
	reader *chain.Reader,
	registry string,
	f feedback,
	already erc8004.Published,
	chainID uint64,
	manager, evidenceBase, identityRegistry string,
	blockNumber uint64,
) (int, error) {
	loanID := f.LoanID.Uint64()
	if already[loanID] {
		log.Printf("loan %d: already published", loanID)
		return 0, nil
	}

	// The registry does NOT refuse feedback for an agent that was never
	// registered: a giveFeedback call naming a nonexistent token id
	// returns receipt status 1 and writes a row attributed to nothing.
	// Observed on Monad testnet, 14 September, when a derivation bug sent
	// four signals to agent 0 and every transaction reported success.
	// Checking ownership first turns that into a loud failure.
	if err := requireRegistered(ctx, rpc, identityRegistry, f.AgentID, blockNumber); err != nil {
		return 0, err
	}

	uri := erc8004.LoanURI(evidenceBase, chainID, manager, loanID)
	held := uint64(0)
	if f.ResolvedAt > f.OriginatedAt {
		held = f.ResolvedAt - f.OriginatedAt
	}

	entries, err := erc8004.Signals(erc8004.LoanOutcome{
		AgentID:       f.AgentID,
		LoanID:        f.LoanID,
		Outcome:       f.Outcome,
		Principal:     f.Principal,
		InterestPaid:  f.InterestPaid,
		PrincipalLoss: f.PrincipalLoss,
		HeldSeconds:   held,
		EvidenceURI:   uri,
		EvidenceHash:  f.EvidenceRef,
		ChainID:       chainID,
		Manager:       manager,
	})
	if err != nil {
		return 0, err
	}

	for _, e := range entries {
		data, err := erc8004.EncodeGiveFeedback(e)
		if err != nil {
			return 0, err
		}
		txHash, err := w.Send(ctx, registry, data)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", e.Tag1, err)
		}
		if err := waitMined(ctx, reader, txHash); err != nil {
			return 0, fmt.Errorf("%s: %w", e.Tag1, err)
		}
		log.Printf("loan %d: %-18s value=%s", loanID, e.Tag1, e.Value)
	}
	log.Printf("loan %d: published %s as %d signals, evidence %s",
		loanID, outcomeName(f.Outcome), len(entries), uri)
	return len(entries), nil
}

// loanAgent reads the agent the loan was originated for.
//
// Attribution derived from the chain rather than supplied (`docs/06`):
// the loan records the binding's agent at origination.
func loanAgent(
	ctx context.Context,
	rpc *chain.RPC,
	manager string,
	loanID, blockNumber uint64,
) (uint64, error) {
	data := append(selector("agentIdOf(uint256)"),
		word(new(big.Int).SetUint64(loanID))...)
	out, err := rpc.CallContract(ctx, manager, data, blockNumber)
	if err != nil || len(out) < 32 {
		// A manager deployed before loans recorded their agent has no
		// such function. Reporting zero lets the caller fall back to an
		// explicit id rather than failing the whole run; publishing
		// still refuses agent 0, so nothing unattributed gets written.
		return 0, nil
	}
	v := new(big.Int).SetBytes(out[:32])
	if !v.IsUint64() {
		return 0, fmt.Errorf("loan %d: agent id does not fit", loanID)
	}
	return v.Uint64(), nil
}

// evidenceHash commits to the obligation and every figure published about
// it.
//
// `docs/06` rejects the previous reference because it hashed only a loan
// number: the same number under a different manager produced the same
// value, so it identified no particular obligation. This binds the chain,
// the manager, the loan, and the reported amounts, so a consumer holding
// the retained record can recompute the hash and see whether the record
// matches what was published. It does not make the record retrievable --
// that is what feedbackURI is for -- but it does make it CHECKABLE.
func evidenceHash(
	chainID uint64,
	manager string,
	loanID uint64,
	outcome uint8,
	principal, interest, loss *big.Int,
	originatedAt, resolvedAt uint64,
) attest.Hash {
	return attest.KeccakText(fmt.Sprintf(
		"credence/feedback/v1\nchain=%d\nmanager=%s\nloan=%d\noutcome=%d\n"+
			"principal=%s\ninterest=%s\nloss=%s\noriginated=%d\nresolved=%d",
		chainID, strings.ToLower(manager), loanID, outcome,
		principal, interest, loss, originatedAt, resolvedAt))
}

// requireRegistered refuses to publish about an agent the identity
// registry does not know.
//
// `docs/06` requires published feedback to be ATTRIBUTABLE. An entry
// naming an unregistered token id attributes an outcome to nobody, and
// the reputation registry will accept it without complaint, so the check
// has to happen here.
func requireRegistered(
	ctx context.Context,
	rpc *chain.RPC,
	identityRegistry string,
	agentID *big.Int,
	blockNumber uint64,
) error {
	if identityRegistry == "" {
		return nil
	}
	if agentID == nil || agentID.Sign() == 0 {
		return fmt.Errorf(
			"refusing to publish for agent 0: an outcome must name a registered agent")
	}
	data, err := erc8004.EncodeOwnerOf(agentID)
	if err != nil {
		return err
	}
	out, err := rpc.CallContract(ctx, identityRegistry, data, blockNumber)
	if err != nil {
		return fmt.Errorf("agent %s is not registered: %w", agentID, err)
	}
	owner, err := erc8004.DecodeAddress(out)
	if err != nil {
		return err
	}
	var zero erc8004.Address
	if owner == zero {
		return fmt.Errorf("agent %s has no owner", agentID)
	}
	return nil
}
