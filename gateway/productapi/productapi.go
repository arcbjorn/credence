// Package productapi is the borrower-facing HTTP surface.
//
// `docs/08` plans a MetaMask plugin "over the existing /v1 product API".
// There was no such API: the gateway served `/health` and `/v1/assess`,
// and the latter exists for the CRE workflow and deliberately does not
// publish. Borrowing and repaying happened through `scripts/` driving
// forge and cast.
//
// # This adds no authority
//
// That is the constraint the package is built around, and it is
// checkable rather than asserted:
//
//   - Every endpoint builds calldata with `gateway/loans` and sends it
//     with `gateway/wallet`. It cannot construct a call the contracts do
//     not already expose.
//   - `LoanManager` checks the oracle's permission, the binding, the
//     account state version and the facility caps on every originate. A
//     request for a loan the oracle did not authorize produces a
//     reverted transaction, exactly as `cast` would.
//   - There is no path here that mints permission, alters a binding, or
//     bypasses the wallet's gas caps.
//
// Demonstrated rather than asserted, 21 September: a repay request from
// a key holding no settlement tokens came back
// `publish_failed: execution reverted: custom error 0x90b8ec18`, which
// is `TransferFailed()`. The chain refused, and the API said so -- it
// did not report a repayment that never happened.
//
// So the API can only ASK. The contracts still decide, which is the
// claim `docs/02` and `docs/05` rest on. If this package could originate
// a loan the chain would refuse, the thesis would be broken to serve a
// convenience.
//
// # Why a separate package from assessd
//
// `assessd` is the CRE route's read side and must not publish: `docs/05`
// pins ONE authority per epoch, and a service that both answered
// "reassess" and sent transactions would be a second one. This package
// publishes and never answers reassessment. They share auth and a
// listener pattern, nothing else.
package productapi

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/loans"
)

// MaxBodyBytes bounds a request. Mirrors the risk service's own limit so
// no caller can make this process buffer more than that one would.
const MaxBodyBytes = 1 << 20

// Publisher sends a transaction and returns its hash.
//
// An interface rather than `*wallet.Wallet` so a test can drive every
// path without a chain, and so this package cannot reach past the
// wallet's gas caps into the signer.
type Publisher interface {
	Send(ctx context.Context, to string, data []byte) (string, error)
}

type Config struct {
	// Manager is the LoanManager this API acts against.
	Manager string
	// Account is the credit account it borrows for.
	Account string
	// AuthToken, when set, is required as `Authorization: Bearer <token>`.
	//
	// This surface SENDS TRANSACTIONS, so an unauthenticated one is a
	// borrow button anyone can press. Empty leaves it open for a local
	// run; `credence-productd` refuses to start that way on a listener
	// that is not loopback.
	AuthToken string
	// Timeout bounds one request's chain work.
	Timeout time.Duration
}

type Server struct {
	cfg    Config
	rpc    *chain.RPC
	wallet Publisher
}

func New(cfg Config, rpc *chain.RPC, w Publisher) *Server {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Server{cfg: cfg, rpc: rpc, wallet: w}
}

// Handler builds the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Open deliberately: a liveness probe that needs the secret is a
	// liveness probe nothing can run.
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/v1/loan", s.authed(s.loan))
	mux.HandleFunc("/v1/borrow", s.authed(s.borrow))
	mux.HandleFunc("/v1/repay", s.authed(s.repay))
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// loan reports the account's credit status, read at one pinned block.
func (s *Server) loan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()

	status, err := loans.Read(ctx, s.rpc, s.cfg.Manager, s.cfg.Account)
	if err != nil {
		writeError(w, http.StatusBadGateway, "chain_read_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// BorrowRequest asks to originate against an accepted assessment.
//
// The caller supplies the binding, and that is SAFE rather than a
// loophole: `LoanManager.originate` calls `oracle.quoteActionHash(binding)`,
// so the oracle -- not this service -- decides whether a binding
// corresponds to an assessment it accepted. A forged or stale binding
// finds no quote and the transaction reverts.
//
// An earlier draft returned 501 here on the theory that accepting a
// binding would let a caller describe an account state the chain never
// agreed to. Reading `LoanManager` showed the opposite: the chain
// validates it, which is precisely why this API can offer the operation
// without holding any authority over it.
type BorrowRequest struct {
	// Principal in settlement base units.
	Principal uint64 `json:"principal"`
	// Binding, as the oracle accepted it. Hex strings, 0x-prefixed.
	Binding BindingJSON `json:"binding"`
}

// BindingJSON is the wire form of `attest.Binding`.
//
// Numbers are strings: a uint256 does not survive a JSON number, and a
// facility id silently truncated by a float64 would bind the loan to a
// different facility.
type BindingJSON struct {
	IdentityRegistryChainID string `json:"identity_registry_chain_id"`
	IdentityRegistry        string `json:"identity_registry"`
	AgentID                 string `json:"agent_id"`
	Operator                string `json:"operator"`
	CreditAccount           string `json:"credit_account"`
	FacilityID              string `json:"facility_id"`
	AccountStateVersion     uint64 `json:"account_state_version"`
	ConfigVersion           uint64 `json:"config_version"`
	ConfigHash              string `json:"config_hash"`
	PolicyEpoch             uint64 `json:"policy_epoch"`
	PolicyHash              string `json:"policy_hash"`
}

// decodeBinding converts the wire form, refusing anything malformed.
//
// Every field is required. A zero-valued default would produce a binding
// that encodes cleanly and describes nothing the oracle ever saw, and
// the revert would name the quote rather than the typo.
func decodeBinding(b BindingJSON) (attest.Binding, error) {
	var out attest.Binding

	num := func(name, v string) (*big.Int, error) {
		n, ok := new(big.Int).SetString(strings.TrimPrefix(v, "0x"), 10)
		if !ok || v == "" {
			return nil, fmt.Errorf("%s: %q is not a decimal integer", name, v)
		}
		return n, nil
	}
	addr := func(name, v string) (attest.Address, error) {
		a, err := loans.ParseAddress(v)
		if err != nil {
			return a, fmt.Errorf("%s: %w", name, err)
		}
		return a, nil
	}
	hash := func(name, v string) (attest.Hash, error) {
		var h attest.Hash
		raw, err := hex.DecodeString(strings.TrimPrefix(v, "0x"))
		if err != nil || len(raw) != 32 {
			return h, fmt.Errorf("%s: %q is not 32 hex bytes", name, v)
		}
		copy(h[:], raw)
		return h, nil
	}

	var err error
	if out.IdentityRegistryChainID, err = num("identity_registry_chain_id", b.IdentityRegistryChainID); err != nil {
		return out, err
	}
	if out.IdentityRegistry, err = addr("identity_registry", b.IdentityRegistry); err != nil {
		return out, err
	}
	if out.AgentID, err = num("agent_id", b.AgentID); err != nil {
		return out, err
	}
	if out.Operator, err = addr("operator", b.Operator); err != nil {
		return out, err
	}
	if out.CreditAccount, err = addr("credit_account", b.CreditAccount); err != nil {
		return out, err
	}
	if out.FacilityID, err = num("facility_id", b.FacilityID); err != nil {
		return out, err
	}
	if out.ConfigHash, err = hash("config_hash", b.ConfigHash); err != nil {
		return out, err
	}
	if out.PolicyHash, err = hash("policy_hash", b.PolicyHash); err != nil {
		return out, err
	}
	out.AccountStateVersion = b.AccountStateVersion
	out.ConfigVersion = b.ConfigVersion
	out.PolicyEpoch = b.PolicyEpoch
	return out, nil
}

type TxResponse struct {
	// Transaction hash. The caller verifies the outcome on chain; this
	// service does not report success on its own say-so.
	Tx string `json:"tx"`
	// What was requested, echoed so a log line is self-contained.
	Action string `json:"action"`
}

func (s *Server) borrow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method)
		return
	}
	var req BorrowRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Principal == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "principal must be positive")
		return
	}

	binding, err := decodeBinding(req.Binding)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_binding", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()

	// The contract decides. `originate` reads the oracle's quote for this
	// exact binding, checks the facility caps, the account's state
	// version and whether a loan is already open, and reverts on any of
	// them. Nothing here anticipates that judgement, and nothing here
	// can bypass it.
	tx, err := s.wallet.Send(ctx, s.cfg.Manager,
		loans.EncodeOriginate(binding, req.Principal))
	if err != nil {
		writeError(w, http.StatusBadGateway, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, TxResponse{
		Tx:     tx,
		Action: fmt.Sprintf("originate(%d)", req.Principal),
	})
}

// RepayRequest settles an active loan.
type RepayRequest struct {
	// LoanID to repay. Zero means "the account's active loan", read
	// from chain rather than assumed.
	LoanID uint64 `json:"loan_id"`
}

func (s *Server) repay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method)
		return
	}
	var req RepayRequest
	if !decode(w, r, &req) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()

	loanID := req.LoanID
	if loanID == 0 {
		status, err := loans.Read(ctx, s.rpc, s.cfg.Manager, s.cfg.Account)
		if err != nil {
			writeError(w, http.StatusBadGateway, "chain_read_failed", err.Error())
			return
		}
		if status.LoanID == 0 {
			writeError(w, http.StatusConflict, "no_active_loan",
				"the account has no active loan to repay")
			return
		}
		loanID = status.LoanID
	}

	// The contract decides whether this succeeds. `repay` reverts on a
	// loan that is not the caller's, already settled, or in recovery,
	// and nothing here anticipates that judgement.
	tx, err := s.wallet.Send(ctx, s.cfg.Manager, loans.EncodeRepay(loanID))
	if err != nil {
		writeError(w, http.StatusBadGateway, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, TxResponse{
		Tx:     tx,
		Action: fmt.Sprintf("repay(%d)", loanID),
	})
}

// authed guards a route with a bearer token, compared in constant time.
func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthToken == "" {
			next(w, r)
			return
		}
		const prefix = "Bearer "
		got := r.Header.Get("Authorization")
		if !strings.HasPrefix(got, prefix) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "")
			return
		}
		presented := []byte(strings.TrimPrefix(got, prefix))
		// Constant time: a byte-by-byte comparison leaks the prefix
		// length through timing, and this token authorizes spending.
		if subtle.ConstantTimeCompare(presented, []byte(s.cfg.AuthToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "")
			return
		}
		next(w, r)
	}
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	// Unknown fields are refused rather than ignored: a caller that
	// misspells `principal` should be told, not silently lent zero.
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, map[string]string{"error": code, "detail": detail})
}
