package main

import "testing"

// A listener that is not loopback must be refused without a token.
//
// The wildcard cases are the ones that matter: ":8090" and "0.0.0.0:8090"
// bind every interface, and both read as "no host given" to a careless
// check. Reported by review, 14 September.
func TestIsLoopback(t *testing.T) {
	for _, c := range []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8090", true},
		{"localhost:8090", true},
		{"[::1]:8090", true},
		{"127.0.0.53:8090", true},

		{":8090", false},        // every interface
		{"0.0.0.0:8090", false}, // every interface, spelled out
		{"[::]:8090", false},    // every interface, v6
		{"192.168.1.10:8090", false},
		{"10.0.0.1:8090", false}, // private, but not host-only
		{"example.com:8090", false},
	} {
		got, err := isLoopback(c.addr)
		if err != nil {
			t.Fatalf("%s: %v", c.addr, err)
		}
		if got != c.want {
			t.Errorf("isLoopback(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

func TestIsLoopbackRejectsUnparseableAddresses(t *testing.T) {
	if _, err := isLoopback("not-an-address"); err == nil {
		t.Fatal("an address with no port must be an error, not a default")
	}
}
