package gatewayrt

import (
	"context"
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
)

// The signed binding must carry the account state version the assessment
// was made against.
//
// The oracle stores `a.binding.accountStateVersion` as the version
// permission is bound to, and `riskAllowedFor` compares that against the
// account's CURRENT version. A signed zero is therefore not a harmless
// default: the update is accepted on chain and then can never match, so
// every monitoring cycle lands and none of them authorizes anything.
//
// Reported by review, 14 September: the assessment binding carried the
// enrolled version while the signing binding left it zero.
func TestLoadBindingCarriesTheWatchedStateVersion(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	registry := store.NewRegistry(db)
	const bindingKey = "0xabc"

	seedBinding(t, db, bindingKey)

	// The account has executed seven times since origination.
	if err := registry.OpenLoan(ctx, bindingKey, "1", 7, 1_000_000); err != nil {
		t.Fatal(err)
	}

	binding, err := LoadBinding(ctx, db, bindingKey)
	if err != nil {
		t.Fatal(err)
	}
	if binding.AccountStateVersion != 7 {
		t.Fatalf("signed binding carries version %d, want the watched 7 -- "+
			"a zero here is accepted on chain and then never matches",
			binding.AccountStateVersion)
	}
}

// A binding with no watched loan has no version to carry. Reporting zero
// there is correct -- there is no standing permission to bind -- but it
// must not be confused with the case above.
func TestLoadBindingWithoutAWatchedLoan(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const bindingKey = "0xdef"
	seedBinding(t, db, bindingKey)

	binding, err := LoadBinding(ctx, db, bindingKey)
	if err != nil {
		t.Fatalf("an unwatched binding must still load: %v", err)
	}
	if binding.AccountStateVersion != 0 {
		t.Fatalf("version = %d, want 0 for an unwatched binding",
			binding.AccountStateVersion)
	}
}

func seedBinding(t *testing.T, db *store.Store, bindingKey string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.DB().ExecContext(ctx,
		`INSERT INTO agents
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
