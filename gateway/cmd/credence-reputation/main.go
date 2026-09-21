// credence-reputation decodes an ERC-8004 registry reply.
//
// `docs/11` asks for an INDEPENDENT reader: the claim is that a third
// party can price this agent's history without trusting Credence. This
// binary takes the raw `eth_call` return and prints what it carries, so
// the check runs against the registry rather than against the gateway
// that wrote to it.
//
// It decodes with `gateway/erc8004`, whose `decode_test.go` is pinned
// against a response the live registry really returned -- a decoder
// checked only against its own encoder agrees with itself about a layout
// both may have wrong.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/arcbjorn/credence/gateway/erc8004"
)

func main() {
	hexPath := flag.String("hex", "", "file holding the raw eth_call return")
	client := flag.String("client", "", "the client address Credence publishes as")
	agent := flag.String("agent", "", "agent id, for the report")
	flag.Parse()

	if *hexPath == "" {
		fail("-hex is required")
	}

	raw, err := os.ReadFile(*hexPath)
	if err != nil {
		fail("read: %v", err)
	}
	body, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(raw)), "0x"))
	if err != nil {
		fail("decode hex: %v", err)
	}

	entries, err := erc8004.DecodeReadAllFeedback(body)
	if err != nil {
		fail("decode reply: %v", err)
	}
	if len(entries) == 0 {
		fail("the registry holds no feedback for agent %s", *agent)
	}

	// Grouped by the loan tag, because that is the unit a reader prices.
	// A flat list of 13 signals does not show that they describe three
	// obligations; the grouping is what makes the history legible.
	loans := map[string][]erc8004.Entry{}
	var revoked, foreign int
	want := strings.ToLower(strings.TrimPrefix(*client, "0x"))

	for _, e := range entries {
		if e.Revoked {
			revoked++
			continue
		}
		if want != "" && !strings.EqualFold(hex.EncodeToString(e.Client[:]), want) {
			// Feedback from another client is legitimate and is NOT
			// counted here: this row checks what Credence published, and
			// folding in a stranger's entries would let anyone inflate
			// the count.
			foreign++
			continue
		}
		loans[e.Tag2] = append(loans[e.Tag2], e)
	}

	tags := make([]string, 0, len(loans))
	for t := range loans {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	fmt.Printf("  %d signal(s) from this client across %d obligation(s)\n",
		len(entries)-revoked-foreign, len(tags))
	if foreign > 0 {
		fmt.Printf("  %d signal(s) from other clients, not counted\n", foreign)
	}
	if revoked > 0 {
		fmt.Printf("  %d revoked, not counted\n", revoked)
	}
	fmt.Println()

	for _, tag := range tags {
		fmt.Printf("  %s\n", tag)
		for _, e := range loans[tag] {
			fmt.Printf("    %-28s %s\n", e.Tag1, scaled(e))
		}
	}

	// A deployment-scoped tag is the anti-farming property `docs/06`
	// claims: `loan:<hash>:N` binds a signal to ONE manager deployment, so
	// a redeployed manager cannot reuse a history it did not earn.
	var scoped []string
	var bare []string
	for _, tag := range tags {
		if strings.Count(tag, ":") >= 2 {
			scoped = append(scoped, tag)
		} else {
			bare = append(bare, tag)
		}
	}
	fmt.Println()
	fmt.Printf("  %d of %d obligation tags are deployment-scoped\n",
		len(scoped), len(tags))

	// A bare `loan:N` predates the scoping and belongs to a superseded
	// manager. It is NOT a failure and it is NOT silently ignored:
	// `ParseLoanTag` refuses to read it as this deployment's, so the
	// registry keeps a record this deployment cannot claim. Saying so is
	// the honest report -- a reader who saw only the count would wonder
	// which tag was short and why.
	if len(bare) > 0 {
		fmt.Printf("  %v predates deployment scoping and is NOT claimed here;\n", bare)
		fmt.Println("    ParseLoanTag rejects it as another deployment's")
	}
	if len(scoped) == 0 {
		fail("no tag is deployment-scoped; a redeployed manager could reuse this history")
	}

	fmt.Println()
	fmt.Println("PASS  the registry alone carries this agent's repayment history")
	fmt.Println()
	fmt.Println("  Read with one eth_call and no Credence service. A third party")
	fmt.Println("  can price this history without trusting the party that wrote it.")
}

// scaled renders a signed fixed-point value with its declared decimals.
func scaled(e erc8004.Entry) string {
	if e.ValueDecimals == 0 {
		return e.Value.String()
	}
	s := e.Value.String()
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	d := int(e.ValueDecimals)
	for len(s) <= d {
		s = "0" + s
	}
	out := s[:len(s)-d] + "." + s[len(s)-d:]
	out = strings.TrimRight(out, "0")
	out = strings.TrimSuffix(out, ".")
	if neg {
		out = "-" + out
	}
	return out
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL  "+format+"\n", args...)
	os.Exit(1)
}
