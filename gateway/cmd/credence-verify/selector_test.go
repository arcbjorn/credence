package main

import (
	"errors"
	"testing"
)

// Nodes report revert data differently, and the beat-6 check compares
// selectors to prove the owner is blocked by DEBT rather than by access
// control. A parser that reads one node's format and silently returns
// "unknown" for another turns that distinction into a failed check on a
// chain where the contracts behaved correctly.
func TestErrorSelectorAcrossNodeFormats(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{
			// anvil
			name: "message carries the selector",
			msg:  "execution reverted, custom error 0xe9d4b4e2",
			want: "0xe9d4b4e2",
		},
		{
			// Monad testnet, observed 14 September: the node decodes the
			// revert bytes as UTF-8, so the message itself is unusable.
			name: "data field with mojibake message",
			msg:  `server returned an error response: error code 3: execution reverted: r˅3, data: "0x72cb8533"`,
			want: "0x72cb8533",
		},
		{
			name: "data field with clean message",
			msg:  `server returned an error response: error code 3: execution reverted, data: "0xe9d4b4e2"`,
			want: "0xe9d4b4e2",
		},
		{
			// An address in the text must never be mistaken for a
			// selector: that would compare a confident wrong value.
			name: "address in message is not a selector",
			msg:  "execution reverted at 0xCaE2590420EAb07eDF737B6069891F36e2e14A34",
			want: "unknown",
		},
		{
			name: "empty revert data",
			msg:  `execution reverted, data: "0x"`,
			want: "unknown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errorSelector(errors.New(c.msg)); got != c.want {
				t.Fatalf("errorSelector(%q) = %q, want %q", c.msg, got, c.want)
			}
		})
	}
	if got := errorSelector(nil); got != "none" {
		t.Fatalf("errorSelector(nil) = %q, want none", got)
	}
}
