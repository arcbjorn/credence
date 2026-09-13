package attest_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/arcbjorn/credence/gateway/attest"
)

// The permitted-action commitment must match the credit account's
// keccak256(abi.encode(adapter, data)) exactly. Pinned against a value
// computed by Foundry: a mismatch makes every execution revert as an
// unpermitted action, which is indistinguishable from a policy refusal.
func TestActionHashMatchesSolidity(t *testing.T) {
	raw, err := hex.DecodeString("8A791620dd6260079BF849Dc5567aDC3F2FdC318")
	if err != nil {
		t.Fatal(err)
	}
	var adapter attest.Address
	copy(adapter[:], raw)
	// cast calldata "acquire(uint256)" 9000000000
	data := append(attest.Selector("acquire(uint256)"),
		attest.Word(big.NewInt(9_000_000_000))...)

	out := attest.AddressWord(adapter)
	out = append(out, attest.Word(big.NewInt(64))...)
	out = append(out, attest.Word(big.NewInt(int64(len(data))))...)
	out = append(out, data...)
	if pad := len(data) % 32; pad != 0 {
		out = append(out, make([]byte, 32-pad)...)
	}
	got := attest.KeccakBytes(out)

	const want = "ddcd89b1c34b1d66cee4993bc6fa484e86479724b2ec66dedb464806751f3ac6"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("got %s, want %s", hex.EncodeToString(got[:]), want)
	}
}
