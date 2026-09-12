// Package riskclient calls the Rust risk service.
//
// `docs/01`: HTTP/JSON is the baseline transport. The client mirrors the
// service's own limits rather than trusting the remote end to enforce them —
// a gateway that can be made to buffer an unbounded response is a gateway
// that can be made to fall over.
//
// Nothing here interprets a decision. The gateway acquires evidence and
// persists results; the judgment stays in the engine.
package riskclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Mirrors risk_service::handler::MAX_BODY_BYTES.
const MaxBodyBytes = 1 << 20

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: timeout},
	}
}

// Assess evaluates a snapshot. `input` and `policy` are passed through
// untouched: the gateway must not reshape what the engine hashes, or the
// retained snapshot would not reproduce the decision.
func (c *Client) Assess(ctx context.Context, input, policy json.RawMessage) (json.RawMessage, error) {
	return c.post(ctx, "/v1/assess", map[string]json.RawMessage{
		"input":  input,
		"policy": policy,
	})
}

// ReplayResult is the service's verdict on a retained assessment.
type ReplayResult struct {
	Matched      bool     `json:"matched"`
	AssessmentID string   `json:"assessment_id"`
	InputHash    string   `json:"input_hash"`
	Differences  []string `json:"differences"`
}

func (c *Client) Replay(
	ctx context.Context,
	assessment, input, policy json.RawMessage,
) (*ReplayResult, error) {
	raw, err := c.post(ctx, "/v1/replay", map[string]json.RawMessage{
		"assessment": assessment,
		"input":      input,
		"policy":     policy,
	})
	if err != nil {
		return nil, err
	}
	var result ReplayResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode replay result: %w", err)
	}
	return &result, nil
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("risk service unreachable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("risk service unhealthy: status %d", res.StatusCode)
	}
	return nil
}

// ServiceError carries the service's own error code, so a caller can
// distinguish a refused replay from a transport failure.
type ServiceError struct {
	Status int
	Code   string `json:"error"`
	Detail string `json:"detail"`
}

func (e *ServiceError) Error() string {
	return fmt.Sprintf("risk service %d %s: %s", e.Status, e.Code, e.Detail)
}

func (c *Client) post(
	ctx context.Context,
	path string,
	payload map[string]json.RawMessage,
) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	// Refuse locally rather than sending a request the service will reject.
	if len(body) > MaxBodyBytes {
		return nil, fmt.Errorf("request body %d exceeds limit %d", len(body), MaxBodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", path, err)
	}
	defer res.Body.Close()

	// Cap the response too. A compromised or buggy service must not be able
	// to exhaust this process.
	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(raw) > MaxBodyBytes {
		return nil, fmt.Errorf("response exceeds limit %d", MaxBodyBytes)
	}

	if res.StatusCode != http.StatusOK {
		svcErr := &ServiceError{Status: res.StatusCode}
		_ = json.Unmarshal(raw, svcErr)
		return nil, svcErr
	}
	return raw, nil
}
