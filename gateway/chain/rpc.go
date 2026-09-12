package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxResponseBytes bounds an RPC reply. A node that returns an unbounded
// body must not be able to exhaust the gateway.
const MaxResponseBytes = 4 << 20

// RPC is a minimal JSON-RPC client.
type RPC struct {
	url  string
	http *http.Client
	// id is not shared state under concurrency: each request carries its own
	// id and the reply is matched by position, since the client issues one
	// request per call.
}

func NewRPC(url string, timeout time.Duration) *RPC {
	return &RPC{url: url, http: &http.Client{Timeout: timeout}}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Call issues one JSON-RPC request and decodes its result.
func (r *RPC) Call(ctx context.Context, method string, params []any, out any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return fmt.Errorf("encode %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", method, err)
	}
	if len(raw) > MaxResponseBytes {
		return fmt.Errorf("%s response exceeds %d bytes", method, MaxResponseBytes)
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode %s: %w", method, err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

// CallContract performs eth_call at a pinned block.
//
// The block is always explicit — never "latest" — so a caller cannot
// accidentally read two facts at different heights (`docs/04`).
func (r *RPC) CallContract(
	ctx context.Context,
	to string,
	data []byte,
	blockNumber uint64,
) ([]byte, error) {
	var result string
	err := r.Call(ctx, "eth_call", []any{
		map[string]string{"to": to, "data": "0x" + hex.EncodeToString(data)},
		hexUint(blockNumber),
	}, &result)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimPrefix(result, "0x"))
}

// Block describes a pinned point in chain history.
type Block struct {
	Number    uint64
	Hash      string
	Timestamp uint64
}

// LatestBlock pins the reference all subsequent reads share.
func (r *RPC) LatestBlock(ctx context.Context) (Block, error) {
	var raw struct {
		Number    string `json:"number"`
		Hash      string `json:"hash"`
		Timestamp string `json:"timestamp"`
	}
	if err := r.Call(ctx, "eth_getBlockByNumber", []any{"latest", false}, &raw); err != nil {
		return Block{}, err
	}
	number, err := parseHexUint(raw.Number)
	if err != nil {
		return Block{}, fmt.Errorf("block number: %w", err)
	}
	timestamp, err := parseHexUint(raw.Timestamp)
	if err != nil {
		return Block{}, fmt.Errorf("block timestamp: %w", err)
	}
	return Block{Number: number, Hash: raw.Hash, Timestamp: timestamp}, nil
}

// SendRawTransaction submits a signed transaction.
func (r *RPC) SendRawTransaction(ctx context.Context, signed []byte) (string, error) {
	var txHash string
	err := r.Call(ctx, "eth_sendRawTransaction",
		[]any{"0x" + hex.EncodeToString(signed)}, &txHash)
	return txHash, err
}

// Receipt reports a transaction's outcome. A nil receipt means unmined,
// which is distinct from a failure and must stay distinguishable.
type Receipt struct {
	Status      uint64
	BlockNumber uint64
	Found       bool
}

func (r *RPC) TransactionReceipt(ctx context.Context, txHash string) (Receipt, error) {
	var raw *struct {
		Status      string `json:"status"`
		BlockNumber string `json:"blockNumber"`
	}
	if err := r.Call(ctx, "eth_getTransactionReceipt", []any{txHash}, &raw); err != nil {
		return Receipt{}, err
	}
	if raw == nil {
		// Unmined. Not an error — the caller must treat this as pending
		// rather than failed, or it will resubmit an in-flight transaction.
		return Receipt{Found: false}, nil
	}
	status, err := parseHexUint(raw.Status)
	if err != nil {
		return Receipt{}, fmt.Errorf("receipt status: %w", err)
	}
	blockNumber, _ := parseHexUint(raw.BlockNumber)
	return Receipt{Status: status, BlockNumber: blockNumber, Found: true}, nil
}

func hexUint(v uint64) string {
	return "0x" + strconv.FormatUint(v, 16)
}

func parseHexUint(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty hex value")
	}
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}
