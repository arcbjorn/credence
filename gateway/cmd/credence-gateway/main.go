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
	"github.com/arcbjorn/credence/gateway/gatewayrt"
	"github.com/arcbjorn/credence/gateway/monitor"
	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/sources"
	"github.com/arcbjorn/credence/gateway/store"
	"github.com/arcbjorn/credence/gateway/wallet"
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

	log.Printf("attestor address 0x%s", hex.EncodeToString(deps.signerAddress[:]))
	log.Printf("operator address %s", deps.operatorAddress)
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
	store           *store.Store
	registry        *store.Registry
	monitor         *monitor.Monitor
	scheduler       *scheduler.Scheduler
	risk            *riskclient.Client
	signerAddress   attest.Address
	operatorAddress string

	// Price feed for the scheduler. Nil when no on-chain source is
	// configured, in which case an absent price keeps the fail-closed
	// staleness path.
	reader      *chain.Reader
	rpc         *chain.RPC
	priceSource string
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
		Vault:       cfg.Addresses.Vault,
		Risky:       cfg.Addresses.Risky,
	}).WithRiskyAsset(chain.RiskyAssetPolicy{
		Price:      cfg.RiskyAsset.Price,
		PriceScale: cfg.RiskyAsset.PriceScale,
		Decimals:   cfg.RiskyAsset.Decimals,
		HaircutBps: cfg.RiskyAsset.HaircutBps,
	}).WithRiskySource(cfg.Addresses.PriceSource).
		WithReceiptWait(cfg.ReceiptWait)

	// The operator key spends gas; the attestation key only signs. Keeping
	// them separate means a compromised attestor cannot move funds.
	operatorKeyBytes, err := config.OperatorKey()
	if err != nil {
		return nil, err
	}
	operatorWallet := wallet.New(secp256k1.PrivKeyFromBytes(operatorKeyBytes), rpc, wallet.Config{
		ChainID:     new(big.Int).SetUint64(cfg.ChainID),
		GasLimitCap: cfg.Wallet.GasLimitCap,
		GasPriceCap: new(big.Int).SetUint64(cfg.Wallet.GasPriceCapWei),
	})

	oracleAddress := cfg.Addresses.Oracle
	pubView := reader.ForPublisher(
		func(ctx context.Context, payload, _ []byte) (string, error) {
			// The payload is already encoded calldata; the signature is
			// inside it, so it is not passed again.
			return operatorWallet.Send(ctx, oracleAddress, payload)
		})
	pub := publisher.New(pubView, store.NewJournal(db))

	evidencePolicyID, err := gatewayrt.ParseHash(cfg.EvidencePolicyID)
	if err != nil {
		return nil, fmt.Errorf("evidence_policy_id: %w", err)
	}
	oracleAddr, err := gatewayrt.ParseAddress(cfg.Addresses.Oracle)
	if err != nil {
		return nil, fmt.Errorf("oracle address: %w", err)
	}

	adapter := attestpub.New(attestpub.Deployment{
		ChainID:           new(big.Int).SetUint64(cfg.ChainID),
		VerifyingContract: oracleAddr,
		DeploymentID:      new(big.Int).SetUint64(cfg.DeploymentID),
		EvidencePolicyID:  evidencePolicyID,
		Route:             attest.RouteDevelopment,
		PolicyContentHash: gatewayrt.PolicyContentHash(cfg.EnginePolicy),
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

	marketReader, err := gatewayrt.BuildMarketReader(cfg, rpc)
	if err != nil {
		return nil, err
	}
	historyReader := sources.NewHistory(
		store.NewDerived(db),
		chain.NewPortfolioReader(rpc, cfg.Addresses.Vault, cfg.StrategyID),
	)

	collector := evidence.New(reader, marketReader, historyReader,
		gatewayrt.BindingLookup(db, cfg), evidence.Config{
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
			publisherFunc{adapter: adapter, store: db},
			recorder{
				store:         db,
				registry:      store.NewRegistry(db),
				schemaVersion: cfg.SchemaVersion,
			},
			monitor.Config{
				Policy: cfg.EnginePolicy,
			}),
		signerAddress:   attest.SignerAddress(signer),
		operatorAddress: operatorWallet.Address(),
		reader:          reader,
		rpc:             rpc,
		priceSource:     cfg.Addresses.PriceSource,
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
			// An absent price is not a shortcut: the scheduler treats it
			// as a staleness trigger, which is the fail-closed behaviour.
			// A configured source turns that into a real observation, so
			// a market move -- not merely the clock -- can trigger
			// reassessment.
			cycles := d.monitor.Tick(ctx, d.prices(ctx), time.Now().Unix())
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

// prices observes the risky asset for every watched binding.
//
// One read serves all bindings because the prototype has a single risky
// asset. A read failure returns no price rather than a stale one: the
// scheduler then reassesses on staleness, which blocks permission, while
// substituting the last known price would let authorization stand on a
// feed that has stopped responding.
func (d *deps) prices(ctx context.Context) map[string]*big.Int {
	out := map[string]*big.Int{}
	if d.priceSource == "" {
		return out
	}

	keys := d.scheduler.Keys()
	if len(keys) == 0 {
		return out
	}

	block, err := d.rpc.LatestBlock(ctx)
	if err != nil {
		log.Printf("price: pin block: %v", err)
		return out
	}
	obs, err := d.reader.RiskyPriceAt(ctx, d.priceSource, block.Number)
	if err != nil {
		log.Printf("price: read source: %v", err)
		return out
	}

	for _, k := range keys {
		out[k] = obs.Price
	}
	return out
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
	binding, err := gatewayrt.LoadBinding(ctx, p.store, bindingKey)
	if err != nil {
		return "", err
	}
	return p.adapter.Publish(ctx, bindingKey, binding, assessment)
}

// recorder persists each cycle so a blocked result is as retrievable as an
// allowed one.
type recorder struct {
	store    *store.Store
	registry *store.Registry
	// schemaVersion labels retained snapshots, so a later reader knows
	// which shape the bytes are in.
	schemaVersion string
}

// RetainDecision stores the canonical snapshot and assessment so the
// decision can be replayed from retained evidence (`docs/04`).
// MarkAccepted records that the chain took this assessment, which is what
// reserves its sequence for the binding (`docs/05`).
func (r recorder) MarkAccepted(ctx context.Context, assessmentID string) error {
	return r.registry.MarkAccepted(ctx, assessmentID)
}

func (r recorder) RetainDecision(
	ctx context.Context,
	bindingKey, inputHash string,
	snapshot, assessment []byte,
) error {
	return r.registry.Retain(ctx, bindingKey, r.schemaVersion,
		inputHash, snapshot, assessment)
}

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
