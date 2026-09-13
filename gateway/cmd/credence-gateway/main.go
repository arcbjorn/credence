// Command credence-gateway runs the continuous underwriting loop.
//
// It owns no judgment. It acquires evidence, asks the engine, signs what
// the engine returned, and delivers it — reconciling before every send.
// Every decision belongs to the pure engine; every enforcement belongs to
// the contracts.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/attestpub"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/config"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/monitor"
	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/store"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func main() {
	configPath := flag.String("config", "gateway.json", "path to the configuration file")
	check := flag.Bool("check", false, "validate configuration and dependencies, then exit")
	flag.Parse()

	if err := run(*configPath, *check); err != nil {
		log.Fatalf("gateway: %v", err)
	}
}

func run(configPath string, checkOnly bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Signals are wired before anything long-running starts, so an
	// interrupt during dependency checks still exits cleanly.
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, err := build(ctx, cfg)
	if err != nil {
		return err
	}
	defer deps.close()

	if err := deps.verify(ctx); err != nil {
		return err
	}

	log.Printf("signer address 0x%s", hex.EncodeToString(deps.signerAddress[:]))
	log.Printf("facility policy %s, class %s", cfg.FacilityPolicy, cfg.RequestedClass)

	// Reload watches before anything ticks. A restart that skips this
	// leaves active loans unmonitored while their on-chain permission
	// continues to stand — the opposite of fail-closed, and invisible.
	tracked, err := deps.rehydrate(ctx)
	if err != nil {
		return err
	}
	log.Printf("tracking %d active loan(s)", tracked)

	if checkOnly {
		log.Print("configuration and dependencies verified")
		return nil
	}

	return deps.loop(ctx, cfg.TickPeriod)
}

type deps struct {
	store         *store.Store
	registry      *store.Registry
	monitor       *monitor.Monitor
	scheduler     *scheduler.Scheduler
	risk          *riskclient.Client
	signerAddress attest.Address
}

func build(ctx context.Context, cfg *config.Config) (*deps, error) {
	keyBytes, err := config.SignerKey()
	if err != nil {
		return nil, err
	}
	signer := secp256k1.PrivKeyFromBytes(keyBytes)

	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	rpc := chain.NewRPC(cfg.RPCURL, cfg.RPCTimeout)
	reader := chain.NewReader(rpc, chain.Addresses{
		Oracle:      cfg.Addresses.Oracle,
		LoanManager: cfg.Addresses.LoanManager,
		Settlement:  cfg.Addresses.Settlement,
	})

	// Submission is deliberately absent for now: signing and nonce
	// management belong to a wallet this deployment does not yet have.
	// A nil submitter fails loudly at the point of sending rather than
	// silently reporting success.
	pubView := reader.ForPublisher(nil)
	pub := publisher.New(pubView, store.NewJournal(db))

	evidencePolicyID, err := parseHash(cfg.EvidencePolicyID)
	if err != nil {
		return nil, fmt.Errorf("evidence_policy_id: %w", err)
	}
	oracleAddr, err := parseAddress(cfg.Addresses.Oracle)
	if err != nil {
		return nil, fmt.Errorf("oracle address: %w", err)
	}

	adapter := attestpub.New(attestpub.Deployment{
		ChainID:           new(big.Int).SetUint64(cfg.ChainID),
		VerifyingContract: oracleAddr,
		DeploymentID:      new(big.Int).SetUint64(cfg.DeploymentID),
		EvidencePolicyID:  evidencePolicyID,
		Route:             attest.RouteDevelopment,
	}, signer, pub)

	sched := scheduler.New(scheduler.Policy{
		PriceMoveBps:          cfg.Scheduler.PriceMoveBps,
		MaxEvidenceAgeSeconds: cfg.Scheduler.MaxEvidenceAgeSeconds,
		RefreshLeadSeconds:    cfg.Scheduler.RefreshLeadSeconds,
		HeartbeatSeconds:      cfg.Scheduler.HeartbeatSeconds,
	}, systemClock{})

	var scenarioID *string
	if cfg.ScenarioID != "" {
		id := cfg.ScenarioID
		scenarioID = &id
	}

	collector := evidence.New(reader, unavailableMarket{}, unavailableHistory{},
		bindingLookup(db), evidence.Config{
			SchemaVersion:   cfg.SchemaVersion,
			FacilityPolicy:  cfg.FacilityPolicy,
			RequestedClass:  cfg.RequestedClass,
			ScenarioID:      scenarioID,
			MaxSkewSeconds:  cfg.Evidence.MaxSkewSeconds,
			ValiditySeconds: cfg.Evidence.ValiditySeconds,
		})

	risk := riskclient.New(cfg.RiskServiceURL, cfg.RiskTimeout)

	return &deps{
		store:     db,
		registry:  store.NewRegistry(db),
		scheduler: sched,
		risk:      risk,
		monitor: monitor.New(sched, collector, risk,
			publisherFunc{adapter: adapter, store: db}, recorder{db}, monitor.Config{
				Policy: cfg.EnginePolicy,
			}),
		signerAddress: attest.SignerAddress(signer),
	}, nil
}

// verify proves every dependency is reachable before the loop starts.
// Failing here is far cheaper than discovering it mid-cycle.
func (d *deps) verify(ctx context.Context) error {
	if err := d.risk.Health(ctx); err != nil {
		return fmt.Errorf("risk service: %w", err)
	}
	mode, foreignKeys, err := d.store.PragmaCheck(ctx)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if mode != "wal" || foreignKeys != 1 {
		return fmt.Errorf("store: expected wal and foreign keys, got %s and %d", mode, foreignKeys)
	}
	return nil
}

// rehydrate reloads the scheduler's watches from the store.
func (d *deps) rehydrate(ctx context.Context) (int, error) {
	watches, err := d.registry.ActiveWatches(ctx)
	if err != nil {
		return 0, fmt.Errorf("reload watches: %w", err)
	}
	for _, w := range watches {
		d.scheduler.Track(scheduler.Watch{
			BindingKey: w.BindingKey,
			LoanID:     w.LoanID,
			// No reference price is known until the first observation, so
			// the scheduler treats the absence as a staleness trigger and
			// reassesses immediately rather than assuming the last price
			// still holds.
			ReferencePrice:      nil,
			PermissionIssuedAt:  w.PermissionIssuedAt,
			EvidenceObservedAt:  w.EvidenceObservedAt,
			AccountStateVersion: w.AccountStateVersion,
		})
	}
	return len(watches), nil
}

func (d *deps) close() {
	if d.store != nil {
		_ = d.store.Close()
	}
}

func (d *deps) loop(ctx context.Context, period time.Duration) error {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	log.Printf("monitoring every %s", period)

	for {
		select {
		case <-ctx.Done():
			log.Print("shutting down")
			return nil
		case <-ticker.C:
			// Prices come from the market reader once one is configured.
			// An empty map is not a shortcut: the scheduler treats an
			// absent price as a staleness trigger, which is the
			// fail-closed behaviour.
			cycles := d.monitor.Tick(ctx, map[string]*big.Int{}, time.Now().Unix())
			for _, c := range cycles {
				if c.Failure != "" {
					log.Printf("cycle %s loan=%s trigger=%s permission=%s failure=%s",
						c.BindingKey, c.LoanID, c.Trigger, c.Permission, c.Failure)
					continue
				}
				log.Printf("cycle %s loan=%s trigger=%s permission=%s tx=%s",
					c.BindingKey, c.LoanID, c.Trigger, c.Permission, c.TxHash)
			}
		}
	}
}

type systemClock struct{}

func (systemClock) Now() int64 { return time.Now().Unix() }

// publisherFunc adapts the attestation adapter to monitor.Publisher.
//
// The on-chain binding is loaded from the store rather than constructed
// here. A fabricated binding would be signed and delivered and would bind
// the assessment to the wrong agent — so an unknown binding is an error,
// never a zero-valued default.
type publisherFunc struct {
	adapter *attestpub.Adapter
	store   *store.Store
}

func (p publisherFunc) Publish(
	ctx context.Context,
	bindingKey string,
	assessment json.RawMessage,
) (string, error) {
	binding, err := loadBinding(ctx, p.store, bindingKey)
	if err != nil {
		return "", err
	}
	return p.adapter.Publish(ctx, bindingKey, binding, assessment)
}

// loadBinding reads the on-chain identity an assessment binds to.
func loadBinding(
	ctx context.Context,
	db *store.Store,
	bindingKey string,
) (attest.Binding, error) {
	var (
		registryChainID            uint64
		registry, agentID          string
		operator, creditAccount    string
		facilityID                 string
		configVersion, policyEpoch uint64
		configHash, policyHash     string
	)
	err := db.DB().QueryRowContext(ctx,
		`SELECT a.identity_registry_chain_id, a.identity_registry, a.agent_id,
		        b.operator, b.credit_account, b.facility_id,
		        b.config_version, b.config_hash, b.policy_epoch, b.policy_hash
		 FROM bindings b JOIN agents a ON a.agent_key = b.agent_key
		 WHERE b.binding_key = ?`, bindingKey,
	).Scan(&registryChainID, &registry, &agentID, &operator, &creditAccount,
		&facilityID, &configVersion, &configHash, &policyEpoch, &policyHash)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("unknown binding %q: %w", bindingKey, err)
	}

	registryAddr, err := parseAddress(registry)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding registry address: %w", err)
	}
	operatorAddr, err := parseAddress(operator)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding operator address: %w", err)
	}
	accountAddr, err := parseAddress(creditAccount)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding credit account: %w", err)
	}
	cfgHash, err := parseHash(configHash)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding config hash: %w", err)
	}
	polHash, err := parseHash(policyHash)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding policy hash: %w", err)
	}

	agent, ok := new(big.Int).SetString(agentID, 10)
	if !ok {
		return attest.Binding{}, fmt.Errorf("agent id %q is not numeric", agentID)
	}
	facility, ok := new(big.Int).SetString(facilityID, 10)
	if !ok {
		return attest.Binding{}, fmt.Errorf("facility id %q is not numeric", facilityID)
	}

	return attest.Binding{
		IdentityRegistryChainID: new(big.Int).SetUint64(registryChainID),
		IdentityRegistry:        registryAddr,
		AgentID:                 agent,
		Operator:                operatorAddr,
		CreditAccount:           accountAddr,
		FacilityID:              facility,
		ConfigVersion:           configVersion,
		ConfigHash:              cfgHash,
		PolicyEpoch:             policyEpoch,
		PolicyHash:              polHash,
	}, nil
}

// recorder persists each cycle so a blocked result is as retrievable as an
// allowed one.
type recorder struct{ store *store.Store }

func (r recorder) RecordCycle(ctx context.Context, c monitor.Cycle) error {
	_, err := r.store.DB().ExecContext(ctx,
		`INSERT INTO jobs (job_id, kind, run_after, attempts, status, payload)
		 VALUES (?, 'monitor_cycle', ?, 1, ?, ?)
		 ON CONFLICT(job_id) DO UPDATE SET
		   attempts = jobs.attempts + 1,
		   status   = excluded.status,
		   payload  = excluded.payload`,
		fmt.Sprintf("%s:%d", c.BindingKey, c.QueuedAt),
		c.QueuedAt,
		cycleStatus(c),
		mustJSON(c),
	)
	return err
}

func cycleStatus(c monitor.Cycle) string {
	if c.Failure != "" {
		return "Failed"
	}
	return "Done"
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

// Placeholder readers. They return errors rather than fabricated values, so
// the engine records Unavailable with a reason instead of deciding on
// invented evidence.
type unavailableMarket struct{}

func (unavailableMarket) Market(context.Context) (evidence.MarketState, error) {
	return evidence.MarketState{}, fmt.Errorf("no market reader configured")
}

type unavailableHistory struct{}

func (unavailableHistory) History(context.Context, string, evidence.BlockRef) (json.RawMessage, error) {
	return nil, fmt.Errorf("no history reader configured")
}

func (unavailableHistory) Forecasts(context.Context, string) (json.RawMessage, error) {
	return nil, fmt.Errorf("no forecast reader configured")
}

func (unavailableHistory) Portfolio(context.Context, evidence.BlockRef) (json.RawMessage, error) {
	return nil, fmt.Errorf("no portfolio reader configured")
}

func bindingLookup(db *store.Store) func(string) (evidence.BindingInfo, bool) {
	return func(bindingKey string) (evidence.BindingInfo, bool) {
		var account string
		err := db.DB().QueryRow(
			`SELECT credit_account FROM bindings WHERE binding_key = ?`, bindingKey,
		).Scan(&account)
		if err != nil {
			return evidence.BindingInfo{}, false
		}
		return evidence.BindingInfo{
			Raw:         json.RawMessage(fmt.Sprintf(`{"binding_key":%q}`, bindingKey)),
			ModelPolicy: json.RawMessage(`{}`),
			Account:     account,
		}, true
	}
}

func parseHash(s string) (attest.Hash, error) {
	var h attest.Hash
	b, err := hex.DecodeString(trim0x(s))
	if err != nil || len(b) != 32 {
		return h, fmt.Errorf("expected 32 hex bytes")
	}
	copy(h[:], b)
	return h, nil
}

func parseAddress(s string) (attest.Address, error) {
	var a attest.Address
	b, err := hex.DecodeString(trim0x(s))
	if err != nil || len(b) != 20 {
		return a, fmt.Errorf("expected 20 hex bytes")
	}
	copy(a[:], b)
	return a, nil
}

func trim0x(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
