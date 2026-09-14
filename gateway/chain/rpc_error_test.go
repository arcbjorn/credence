package chain

import (
	"strings"
	"testing"
)

// A custom error's selector arrives only in the JSON-RPC `data` field.
// Monad additionally decodes those bytes into `message` as text, which
// renders as mojibake -- so a client that keeps only the message cannot
// tell one custom error from another, and every revert looks the same.
func TestRPCErrorKeepsRevertData(t *testing.T) {
	e := &rpcError{
		Code:    3,
		Message: "execution reverted: r˅3",
		Data:    "0x72cb8533",
	}
	got := e.Error()
	if !strings.Contains(got, "0x72cb8533") {
		t.Fatalf("revert data dropped from %q", got)
	}
}

// Empty revert data carries no selector, so it must not be presented as
// though it did.
func TestRPCErrorOmitsEmptyData(t *testing.T) {
	for _, d := range []string{"", "0x"} {
		e := &rpcError{Code: 3, Message: "execution reverted", Data: d}
		if strings.Contains(e.Error(), "data:") {
			t.Fatalf("empty data %q rendered as data field: %q", d, e.Error())
		}
	}
}
