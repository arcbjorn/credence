package riskclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arcbjorn/credence/gateway/riskclient"
)

func fixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "performer", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return json.RawMessage(raw)
}

// A stub standing in for the Rust service, so client behaviour is tested
// without requiring a build of the other language.
func stub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestAssessReturnsRawAssessment(t *testing.T) {
	s := stub(t, http.StatusOK, `{"assessment_id":"a1","outcome":{"decision":"Unavailable"}}`)
	c := riskclient.New(s.URL, 2*time.Second)

	raw, err := c.Assess(context.Background(), fixture(t, "input.json"), fixture(t, "policy.json"))
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if !strings.Contains(string(raw), "assessment_id") {
		t.Fatalf("unexpected body: %s", raw)
	}
}

func TestReplayDecodesResult(t *testing.T) {
	s := stub(t, http.StatusOK,
		`{"matched":true,"assessment_id":"a1","input_hash":"h1","differences":[]}`)
	c := riskclient.New(s.URL, 2*time.Second)

	res, err := c.Replay(context.Background(),
		fixture(t, "assessment.json"), fixture(t, "input.json"), fixture(t, "policy.json"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !res.Matched || res.AssessmentID != "a1" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// A refused replay must be distinguishable from a transport failure, so the
// caller can tell "wrong pins" from "service down".
func TestServiceErrorCarriesCode(t *testing.T) {
	s := stub(t, http.StatusConflict,
		`{"error":"model_mismatch","detail":"different models"}`)
	c := riskclient.New(s.URL, 2*time.Second)

	_, err := c.Replay(context.Background(),
		fixture(t, "assessment.json"), fixture(t, "input.json"), fixture(t, "policy.json"))
	if err == nil {
		t.Fatal("expected an error")
	}
	var svcErr *riskclient.ServiceError
	if !errorsAs(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "model_mismatch" || svcErr.Status != http.StatusConflict {
		t.Fatalf("unexpected error: %+v", svcErr)
	}
}

// A buggy or compromised service must not be able to exhaust this process.
func TestOversizedResponseIsRefused(t *testing.T) {
	s := stub(t, http.StatusOK, strings.Repeat("x", riskclient.MaxBodyBytes+10))
	c := riskclient.New(s.URL, 5*time.Second)

	_, err := c.Assess(context.Background(), fixture(t, "input.json"), fixture(t, "policy.json"))
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected a size refusal, got %v", err)
	}
}

func TestUnreachableServiceIsReported(t *testing.T) {
	c := riskclient.New("http://127.0.0.1:1", 200*time.Millisecond)
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("expected an unreachable error")
	}
}

func TestContextCancellationIsHonoured(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := riskclient.New(s.URL, 5*time.Second)
	if err := c.Health(ctx); err == nil {
		t.Fatal("expected cancellation to surface")
	}
}

func errorsAs(err error, target any) bool {
	type as interface{ As(any) bool }
	for err != nil {
		if e, ok := err.(as); ok && e.As(target) {
			return true
		}
		if t, ok := target.(**riskclient.ServiceError); ok {
			if se, ok := err.(*riskclient.ServiceError); ok {
				*t = se
				return true
			}
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
