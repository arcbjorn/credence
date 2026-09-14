// Command credence-assessd serves monitoring assessments over HTTP.
//
// The CRE route's counterpart to `credence-gateway`. That daemon assesses
// AND publishes, signing with a development key; this one only assesses.
//
// **They are deliberately separate processes.** `docs/05` pins one
// authorization route per facility epoch, and a CRE facility rejects
// development attestations outright. A single process that could both sign
// an assessment and hand one to a DON would be two authorities for the same
// facility -- exactly the arrangement the pinned route exists to prevent.
// Keeping them apart means a CRE deployment runs this binary and never
// holds an attestation key at all.
//
// This process therefore has no signer and no publisher. It collects
// evidence, calls the engine, normalizes the answer, retains it, and
// returns it. Everything that decides is the engine's; everything that
// enforces is on chain.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/arcbjorn/credence/gateway/assessd"
	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/attestpub"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/config"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/gatewayrt"
	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/sources"
	"github.com/arcbjorn/credence/gateway/store"
)

func main() {
	configPath := flag.String("config", "gateway.json",
		"path to the configuration file")
	addr := flag.String("addr", "127.0.0.1:8090", "listen address")
	loansFlag := flag.String("loans", "",
		"comma-separated loan ids to serve, each as loanID=bindingKey")
	check := flag.Bool("check", false,
		"validate configuration and dependencies, then exit")
	flag.Parse()

	// The token is read from the environment, never a flag: a flag value
	// is visible in `ps` to every other user on the host.
	token := os.Getenv("CREDENCE_ASSESSD_TOKEN")

	if err := run(*configPath, *addr, *loansFlag, token, *check); err != nil {
		log.Fatalf("assessd: %v", err)
	}
}

func run(configPath, addr, loansFlag, authToken string, checkOnly bool) error {
	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

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

	marketReader, err := gatewayrt.BuildMarketReader(cfg, rpc)
	if err != nil {
		return err
	}
	historyReader := sources.NewHistory(
		store.NewDerived(db),
		chain.NewPortfolioReader(rpc, cfg.Addresses.Vault, cfg.StrategyID),
	)

	var scenarioID *string
	if cfg.ScenarioID != "" {
		id := cfg.ScenarioID
		scenarioID = &id
	}

	lookup := gatewayrt.BindingLookup(db, cfg)
	collector := evidence.New(reader, marketReader, historyReader, lookup,
		evidence.Config{
			SchemaVersion:   cfg.SchemaVersion,
			FacilityPolicy:  cfg.FacilityPolicy,
			RequestedClass:  cfg.RequestedClass,
			ScenarioID:      scenarioID,
			MaxSkewSeconds:  cfg.Evidence.MaxSkewSeconds,
			ValiditySeconds: cfg.Evidence.ValiditySeconds,
		})

	risk := riskclient.New(cfg.RiskServiceURL, cfg.RiskTimeout)

	// The normalizer, with no signer.
	//
	// `attestpub.Adapter` signs when it publishes; `Envelope` stops before
	// that. Passing nil for the key and the publisher is what makes it
	// impossible for this process to attest anything: there is no key here
	// to misuse, and calling Publish would panic rather than quietly sign.
	oracleAddr, err := gatewayrt.ParseAddress(cfg.Addresses.Oracle)
	if err != nil {
		return fmt.Errorf("oracle address: %w", err)
	}
	evidencePolicyID, err := gatewayrt.ParseHash(cfg.EvidencePolicyID)
	if err != nil {
		return fmt.Errorf("evidence policy id: %w", err)
	}
	normalizer := attestpub.New(attestpub.Deployment{
		ChainID:           new(big.Int).SetUint64(cfg.ChainID),
		VerifyingContract: oracleAddr,
		DeploymentID:      new(big.Int).SetUint64(cfg.DeploymentID),
		EvidencePolicyID:  evidencePolicyID,
		// The route this deployment speaks for. A CRE facility rejects a
		// development envelope outright, so declaring it here means a
		// misconfiguration fails at the oracle rather than silently
		// producing fields for the wrong route.
		Route:             attest.RouteCRE,
		PolicyContentHash: gatewayrt.PolicyContentHash(cfg.EnginePolicy),
	}, nil, nil)

	bindings, err := parseLoans(ctx, loansFlag, db)
	if err != nil {
		return err
	}
	if len(bindings) == 0 {
		return errors.New(
			"no loans configured: pass -loans loanID=bindingKey, so a caller " +
				"cannot name an obligation this deployment does not monitor")
	}

	// A public listener without a credential is a permission oracle anyone
	// can drive. Loopback is a real mitigation and the default, but it is
	// not an access control, so anything else must present a token.
	//
	// Refused at startup rather than warned about: a warning in a log is
	// exactly what nobody reads before leaving a service exposed.
	if authToken == "" {
		local, err := isLoopback(addr)
		if err != nil {
			return fmt.Errorf("parse -addr %q: %w", addr, err)
		}
		if !local {
			return fmt.Errorf(
				"refusing to serve on %s without authentication: set "+
					"CREDENCE_ASSESSD_TOKEN, or bind to loopback", addr)
		}
	}

	server := assessd.New(collector, risk, normalizer, bindings,
		recorder{store: db, registry: store.NewRegistry(db),
			schemaVersion: cfg.SchemaVersion},
		assessd.Config{Policy: cfg.EnginePolicy, AuthToken: authToken})

	// Prove every dependency is reachable before serving. Failing here is
	// far cheaper than discovering it mid-request, when the caller's only
	// correct response is to publish nothing.
	if err := risk.Health(ctx); err != nil {
		return fmt.Errorf("risk service unreachable: %w", err)
	}
	if checkOnly {
		log.Printf("configuration valid; %d loan(s) served", len(bindings))
		return nil
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("serving %d loan(s) on %s", len(bindings), addr)
	if err := srv.ListenAndServe(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// parseLoans resolves the operator's loan list into bindings.
//
// The operator says which loans are served and what they are, mirroring
// `CREReceiver.enrol` on chain. Resolving from the store rather than from
// the flag means the binding is the one this deployment actually
// originated under -- a flag that carried binding fields would let a
// typo produce assessments for the wrong account.
func parseLoans(
	ctx context.Context,
	spec string,
	db *store.Store,
) (assessd.StaticBindings, error) {
	out := assessd.StaticBindings{}
	if spec == "" {
		return out, nil
	}
	for _, pair := range splitNonEmpty(spec, ',') {
		eq := indexByte(pair, '=')
		if eq <= 0 || eq == len(pair)-1 {
			return nil, fmt.Errorf(
				"malformed -loans entry %q, want loanID=bindingKey", pair)
		}
		loanID, err := strconv.ParseUint(pair[:eq], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("loan id in %q: %w", pair, err)
		}
		if loanID == 0 {
			return nil, fmt.Errorf("loan id must be non-zero in %q", pair)
		}
		// Zero here, and replaced per request: the version an assessment
		// binds to is the one the engine SAW, which is read fresh in
		// `Assess` rather than frozen at configuration time.
		binding, err := gatewayrt.LoadBinding(ctx, db, pair[eq+1:], 0)
		if err != nil {
			return nil, fmt.Errorf("loan %d: %w", loanID, err)
		}
		out[loanID] = binding
	}
	return out, nil
}

func splitNonEmpty(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == sep {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// recorder persists the snapshot and assessment a decision came from.
//
// `docs/04` requires a decision be replayable whether or not it reached
// chain -- and on this route it never does directly, so retention here is
// the only record that the assessment was made at all.
type recorder struct {
	store         *store.Store
	registry      *store.Registry
	schemaVersion string
}

func (r recorder) RetainDecision(
	ctx context.Context,
	bindingKey, inputHash string,
	snapshot, assessment []byte,
) error {
	return r.registry.Retain(ctx, bindingKey, r.schemaVersion,
		inputHash, snapshot, assessment)
}

// isLoopback reports whether this listen address is reachable only from
// the host.
//
// An empty or wildcard host ("", "0.0.0.0", "::") is NOT loopback: those
// bind every interface, which is the case this check exists to catch.
func isLoopback(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, err
	}
	if host == "" {
		// ":8090" binds every interface.
		return false, nil
	}
	if host == "localhost" {
		return true, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname that is not "localhost" could resolve anywhere.
		// Treat it as public rather than resolving it here, since the
		// answer could differ by the time the listener binds.
		return false, nil
	}
	return ip.IsLoopback(), nil
}
