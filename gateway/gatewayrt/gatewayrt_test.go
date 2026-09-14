package gatewayrt

import (
	"context"
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
)

// The binding carries the version the CALLER assessed, not one read from
// the store.
//
// Three places compare it: the engine refuses a snapshot whose binding and
// account evidence disagree, the oracle stores it as the version
// permission applies to, and `riskAllowedFor` compares that against the
// account's current version at execution time.
//
// An earlier fix read the WATCHED version here -- the version permission
// was last bound to, frozen at enrolment. That is right for the first
// cycle and wrong for every one after the account executes again, at which
// point the engine rejects every assessment and monitoring deadlocks.
// Reported by review, 14 September.
func TestLoadBindingCarriesTheSuppliedStateVersion(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const bindingKey = "0xabc"
	seedBinding(t, db, bindingKey)

	// A watch exists at version 3, and it must NOT be what gets signed:
	// the account has executed since, and the assessment is about the
	// account as it stands now.
	if err := store.NewRegistry(db).OpenLoan(
		ctx, bindingKey, "1", 3, 1_000_000); err != nil {
		t.Fatal(err)
	}

	binding, err := LoadBinding(ctx, db, bindingKey, 9)
	if err != nil {
		t.Fatal(err)
	}
	if binding.AccountStateVersion != 9 {
		t.Fatalf("signed binding carries version %d, want the assessed 9 "+
			"(the stale watch says 3)", binding.AccountStateVersion)
	}
}

// AccountFor resolves the account a binding names, so a caller can read
// its live state version before building the binding.
func TestAccountForResolvesTheBinding(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const bindingKey = "0xdef"
	seedBinding(t, db, bindingKey)

	account, err := AccountFor(ctx, db, bindingKey)
	if err != nil {
		t.Fatal(err)
	}
	if account != "0x00000000000000000000000000000000000000ac" {
		t.Fatalf("account = %q", account)
	}

	if _, err := AccountFor(ctx, db, "0xunknown"); err == nil {
		t.Fatal("an unknown binding must not resolve to an empty account")
	}
}

func seedBinding(t *testing.T, db *store.Store, bindingKey string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.DB().ExecContext(ctx,
		`INSERT OR IGNORE INTO agents
		   (agent_key, identity_registry_chain_id, identity_registry, agent_id,
		    created_at)
		 VALUES ('a1', 10143, '0x00000000000000000000000000000000000000ae',
		         '1830', 1000000)`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().ExecContext(ctx,
		`INSERT INTO bindings
		   (binding_key, agent_key, operator, credit_account, facility_id,
		    config_version, config_hash, policy_epoch, policy_hash, created_at)
		 VALUES (?, 'a1',
		    '0x00000000000000000000000000000000000000a9',
		    '0x00000000000000000000000000000000000000ac',
		    '1', 1,
		    '0x1111111111111111111111111111111111111111111111111111111111111111',
		    1,
		    '0x2222222222222222222222222222222222222222222222222222222222222222',
		    1000000)`,
		bindingKey,
	); err != nil {
		t.Fatal(err)
	}
}
