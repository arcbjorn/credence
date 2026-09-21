package productapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/loans"
	"github.com/arcbjorn/credence/gateway/productapi"
)

// recorder captures what would have been sent, so every path can be
// driven without a chain -- and so a test can assert on the CALLDATA,
// which is the only thing this service actually decides.
type recorder struct {
	to   string
	data []byte
	err  error
}

func (r *recorder) Send(_ context.Context, to string, data []byte) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	r.to, r.data = to, append([]byte{}, data...)
	return "0xdeadbeef", nil
}

const manager = "0x7e5F4552091A69125d5DfCb7b8C2659029395Bdf"
const account = "0x2B5AD5c4795c026514f8317c7a215E218DcCD6cF"

func serve(t *testing.T, w productapi.Publisher, token string) (*httptest.Server, *recorder) {
	t.Helper()
	rec, _ := w.(*recorder)
	s := productapi.New(productapi.Config{
		Manager:   manager,
		Account:   account,
		AuthToken: token,
	}, nil, w)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, rec
}

func post(t *testing.T, srv *httptest.Server, path, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// --- the property the package exists to preserve ---

// The API must send EXACTLY the calldata the contract expects, and
// nothing else. If it could construct a different call, "the contracts
// decide" would be a claim rather than a fact.
func TestBorrowSendsTheContractsOwnCalldata(t *testing.T) {
	rec := &recorder{}
	srv, _ := serve(t, rec, "")

	res := post(t, srv, "/v1/borrow", "", validBorrow(4_000_000_000))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}

	if !strings.EqualFold(rec.to, manager) {
		t.Fatalf("sent to %s, not the loan manager", rec.to)
	}

	// Byte-identical to what `loans.EncodeOriginate` produces for the
	// same binding: the service adds nothing of its own.
	want := loans.EncodeOriginate(testBinding(), 4_000_000_000)
	if !bytes.Equal(rec.data, want) {
		t.Fatalf("calldata differs from the encoder's\n got %x\nwant %x", rec.data, want)
	}
}

// A malformed binding is refused before anything is sent. The chain
// would refuse it too, but a service that broadcast garbage would spend
// gas to learn what it could have checked for free.
func TestMalformedBindingIsRefusedWithoutSending(t *testing.T) {
	for _, bad := range []struct{ name, body string }{
		{"short address", strings.Replace(validBorrow(1), account, "0x1234", 1)},
		{"bad hash", strings.Replace(validBorrow(1), strings.Repeat("11", 32), "zz", 1)},
		{"empty chain id", strings.Replace(validBorrow(1), `"10143"`, `""`, 1)},
	} {
		t.Run(bad.name, func(t *testing.T) {
			rec := &recorder{}
			srv, _ := serve(t, rec, "")

			res := post(t, srv, "/v1/borrow", "", bad.body)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", res.StatusCode)
			}
			if rec.data != nil {
				t.Fatal("a malformed binding must not reach the chain")
			}
		})
	}
}

// Zero principal is refused: `originate` reverts on it, and asking the
// chain to say so costs gas for a request this service can read.
func TestZeroPrincipalIsRefused(t *testing.T) {
	rec := &recorder{}
	srv, _ := serve(t, rec, "")

	res := post(t, srv, "/v1/borrow", "", validBorrow(0))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", res.StatusCode)
	}
	if rec.data != nil {
		t.Fatal("a zero principal must not reach the chain")
	}
}

// --- repay ---

func TestRepaySendsTheEncodedLoanID(t *testing.T) {
	rec := &recorder{}
	srv, _ := serve(t, rec, "")

	res := post(t, srv, "/v1/repay", "", `{"loan_id":7}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if !bytes.Equal(rec.data, loans.EncodeRepay(7)) {
		t.Fatalf("calldata %x is not repay(7)", rec.data)
	}
}

// A publish failure is reported as a failure. A service that returned
// 200 on a send it could not complete would have the caller believe a
// loan was repaid when nothing reached the chain.
func TestPublishFailureIsNotReportedAsSuccess(t *testing.T) {
	rec := &recorder{err: fmt.Errorf("no gas")}
	srv, _ := serve(t, rec, "")

	res := post(t, srv, "/v1/repay", "", `{"loan_id":7}`)
	if res.StatusCode == http.StatusOK {
		t.Fatal("a failed publish must not return 200")
	}
}

// --- auth ---

// This surface SENDS TRANSACTIONS, so an unauthenticated one is a borrow
// button anyone can press.
func TestTokenIsRequiredWhenConfigured(t *testing.T) {
	rec := &recorder{}
	srv, _ := serve(t, rec, "secret")

	for _, presented := range []string{"", "wrong", "secre", "secrett"} {
		res := post(t, srv, "/v1/repay", presented, `{"loan_id":7}`)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q got status %d, want 401", presented, res.StatusCode)
		}
	}
	if rec.data != nil {
		t.Fatal("an unauthorized request must not reach the chain")
	}

	if res := post(t, srv, "/v1/repay", "secret", `{"loan_id":7}`); res.StatusCode != http.StatusOK {
		t.Fatalf("the correct token got status %d", res.StatusCode)
	}
}

// Health stays open: a liveness probe that needs the secret is a
// liveness probe nothing can run.
func TestHealthNeedsNoToken(t *testing.T) {
	srv, _ := serve(t, &recorder{}, "secret")

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health returned %d", res.StatusCode)
	}
}

// A misspelled field is reported rather than ignored. Silently lending
// zero because `principal` was typed `principle` is the worst outcome.
func TestUnknownFieldIsRefused(t *testing.T) {
	rec := &recorder{}
	srv, _ := serve(t, rec, "")

	res := post(t, srv, "/v1/repay", "", `{"loan_id":7,"principle":5}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", res.StatusCode)
	}
	if rec.data != nil {
		t.Fatal("an unrecognised field must not reach the chain")
	}
}

func TestWrongMethodIsRefused(t *testing.T) {
	srv, _ := serve(t, &recorder{}, "")

	res, err := http.Get(srv.URL + "/v1/repay")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/repay returned %d", res.StatusCode)
	}
}

// --- helpers ---

func testBinding() attest.Binding {
	reg, _ := loans.ParseAddress(manager)
	op, _ := loans.ParseAddress(account)
	acct, _ := loans.ParseAddress(account)

	var ch, ph attest.Hash
	for i := range ch {
		ch[i], ph[i] = 0x11, 0x22
	}
	b := attest.Binding{
		IdentityRegistry:    reg,
		Operator:            op,
		CreditAccount:       acct,
		AccountStateVersion: 3,
		ConfigVersion:       1,
		ConfigHash:          ch,
		PolicyEpoch:         1,
		PolicyHash:          ph,
	}
	b.IdentityRegistryChainID = big.NewInt(10143)
	b.AgentID = big.NewInt(1830)
	b.FacilityID = big.NewInt(1)
	return b
}

func validBorrow(principal uint64) string {
	body, _ := json.Marshal(map[string]any{
		"principal": principal,
		"binding": map[string]any{
			"identity_registry_chain_id": "10143",
			"identity_registry":          manager,
			"agent_id":                   "1830",
			"operator":                   account,
			"credit_account":             account,
			"facility_id":                "1",
			"account_state_version":      3,
			"config_version":             1,
			"config_hash":                "0x" + strings.Repeat("11", 32),
			"policy_epoch":               1,
			"policy_hash":                "0x" + strings.Repeat("22", 32),
		},
	})
	return string(body)
}
