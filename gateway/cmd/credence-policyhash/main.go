package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/arcbjorn/credence/gateway/gatewayrt"
)

func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(gatewayrt.PolicyContentHash(json.RawMessage(raw)))
}
