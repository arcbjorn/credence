package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Pack is one evaluation run, recorded completely enough to reproduce.
//
// `docs/07`: "Each run records generator version, seed, input
// distribution, schedule, agent configuration, model and policy hashes,
// and all generated events. A fixture pack is immutable once used in a
// recorded demonstration."
//
// Immutability is enforced by a digest rather than by convention. A pack
// whose contents no longer hash to its recorded digest is not the pack
// that produced the demonstration, and `Verify` says so — which is the
// difference between a claim of immutability and a checkable one.
type Pack struct {
	// Name distinguishes a development set from an evaluation set. Their
	// seeds and shock schedules must differ (`docs/07`), and a run that
	// tuned against one and reported the other is the whole reason the
	// distinction exists.
	Name string   `json:"name"`
	Kind PackKind `json:"kind"`

	// GeneratorVersion, with the seed, identifies the world. A pack
	// replayed under a different generator version is a different world
	// however identical its config reads.
	GeneratorVersion string `json:"generator_version"`
	World            Config `json:"world"`
	WorldFingerprint string `json:"world_fingerprint"`

	// ModelHash and PolicyHash identify what was being evaluated. A pack
	// without them records a result nobody can attribute.
	ModelHash  string `json:"model_hash"`
	PolicyHash string `json:"policy_hash"`

	// AgentConfig is whatever the runner was configured with, opaque here
	// so the pack does not have to know about agent internals.
	AgentConfig json.RawMessage `json:"agent_config,omitempty"`

	Events  []Event `json:"events"`
	Outcome Outcome `json:"outcome"`

	// Digest covers every field above. Written last, checked first.
	Digest string `json:"digest"`
}

type PackKind string

const (
	// KindDevelopment is tuned against. Results from it are not evidence.
	KindDevelopment PackKind = "development"
	// KindEvaluation is run ONCE against a model tuned elsewhere.
	KindEvaluation PackKind = "evaluation"
)

// Outcome is what the run realized. Reported, never scored.
//
// `docs/07`: "Do not claim default-model validation from a small synthetic
// population. Report what was exercised." So these are counts and sums a
// reader can check, with no aggregate quality number anywhere — a single
// score is exactly the thing a small population cannot support.
type Outcome struct {
	Originated int `json:"originated"`
	Repaid     int `json:"repaid"`
	Recovered  int `json:"recovered"`
	Defaulted  int `json:"defaulted"`
	Declined   int `json:"declined"`

	// PrincipalLent and the three resolutions below reconcile:
	//   lent == repaid + recovered + lost
	// A pack where they do not is reporting an accounting error, not a
	// risk result.
	PrincipalLent      uint64 `json:"principal_lent"`
	PrincipalRepaid    uint64 `json:"principal_repaid"`
	PrincipalRecovered uint64 `json:"principal_recovered"`
	PrincipalLost      uint64 `json:"principal_lost"`
	InterestCollected  uint64 `json:"interest_collected"`

	// ShocksExercised names the shocks that actually occurred during a
	// live loan. A shock scheduled while nothing was outstanding tested
	// nothing, and counting it would overstate coverage.
	ShocksExercised []string `json:"shocks_exercised"`

	// UnnamedShockExercised records whether a shock OUTSIDE the model's
	// named scenarios occurred while a loan was live. `docs/07` requires
	// one, because it is what separates expected stress protection from
	// guaranteed recovery.
	UnnamedShockExercised bool `json:"unnamed_shock_exercised"`
}

// Reconciles reports whether the principal figures add up.
func (o Outcome) Reconciles() bool {
	return o.PrincipalLent ==
		o.PrincipalRepaid+o.PrincipalRecovered+o.PrincipalLost
}

// Seal computes the pack's digest and returns a sealed copy.
//
// Called once, when the run finishes. Sealing a pack that is still being
// written would record a digest for a state nothing else will ever have.
func (p Pack) Seal() (Pack, error) {
	p.Digest = ""
	sum, err := p.digest()
	if err != nil {
		return Pack{}, err
	}
	p.Digest = sum
	return p, nil
}

// Verify reports whether the pack still hashes to its recorded digest.
//
// `docs/07` makes a pack immutable once used in a recorded demonstration.
// This is what makes that checkable: an edited pack fails here, and a
// result quoted from a failing pack is a result from a different run.
func (p Pack) Verify() error {
	recorded := p.Digest
	if recorded == "" {
		return fmt.Errorf("pack %q carries no digest", p.Name)
	}
	p.Digest = ""
	sum, err := p.digest()
	if err != nil {
		return err
	}
	if sum != recorded {
		return fmt.Errorf(
			"pack %q has been modified: digest %s, recorded %s",
			p.Name, sum, recorded)
	}
	return nil
}

func (p Pack) digest() (string, error) {
	// Canonical JSON: Go's encoder sorts map keys, and every slice here
	// is already in deterministic order, so the same pack hashes the same
	// on any machine.
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("hash pack: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Save writes a sealed pack to disk.
//
// Refuses to overwrite: `docs/07` makes a used pack immutable, and the
// cheapest enforcement is that the file cannot be replaced in place.
func (p Pack) Save(dir string) (string, error) {
	if p.Digest == "" {
		return "", fmt.Errorf("pack %q must be sealed before saving", p.Name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, p.Name+".json")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf(
			"pack %q already exists at %s; a used pack is immutable", p.Name, path)
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Load reads a pack and verifies it has not been modified.
func Load(path string) (Pack, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, err
	}
	var p Pack
	if err := json.Unmarshal(raw, &p); err != nil {
		return Pack{}, fmt.Errorf("decode pack: %w", err)
	}
	if err := p.Verify(); err != nil {
		return Pack{}, err
	}
	return p, nil
}

// Summarize renders a pack as the report `docs/07` asks for.
//
// Coverage and realized figures, with no aggregate score: a small
// synthetic population cannot support one, and printing a number that
// looks like a grade invites exactly the claim the document forbids.
func (p Pack) Summarize() string {
	o := p.Outcome
	counts := Count(p.Events)

	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)

	s := fmt.Sprintf(`pack            %s (%s)
generator       %s, seed %d
world           %d days, fingerprint %s
model / policy  %s / %s

outcomes        originated=%d repaid=%d recovered=%d defaulted=%d declined=%d
principal       lent=%d repaid=%d recovered=%d lost=%d
interest        collected=%d
reconciles      %v
`,
		p.Name, p.Kind,
		p.GeneratorVersion, p.World.Seed,
		p.World.Days, p.WorldFingerprint,
		p.ModelHash, p.PolicyHash,
		o.Originated, o.Repaid, o.Recovered, o.Defaulted, o.Declined,
		o.PrincipalLent, o.PrincipalRepaid, o.PrincipalRecovered, o.PrincipalLost,
		o.InterestCollected,
		o.Reconciles(),
	)

	s += "\nevents exercised\n"
	for _, k := range kinds {
		s += fmt.Sprintf("  %-18s %d\n", k, counts[EventKind(k)])
	}

	s += "\nshocks exercised while a loan was live\n"
	if len(o.ShocksExercised) == 0 {
		s += "  NONE -- no shock occurred against an outstanding loan,\n" +
			"  so this run did not test the stress path at all\n"
	}
	for _, sh := range o.ShocksExercised {
		s += fmt.Sprintf("  %s\n", sh)
	}
	if !o.UnnamedShockExercised {
		s += "\n  NOTE: no shock outside the model's named scenarios occurred.\n" +
			"  This run shows expected stress protection, NOT guaranteed\n" +
			"  recovery (`docs/07`).\n"
	} else {
		// The comparison `docs/07` exists to surface. Stated with the
		// realized figure rather than left for a reader to compute, since
		// the whole point is that it is easy to overlook.
		if o.PrincipalLent > 0 {
			lostPct := o.PrincipalLost * 100 / o.PrincipalLent
			s += fmt.Sprintf(
				"\n  A shock OUTSIDE the model's named scenarios occurred while\n"+
					"  a loan was live, and %d%% of principal was lost against it.\n"+
					"  The model stresses to the scenarios it names; this is what\n"+
					"  it does when reality supplies one it does not (`docs/07`).\n",
				lostPct)
		}
	}

	s += "\nThis is a synthetic population. It reports what was exercised;\n" +
		"it is not default-model validation (`docs/07`).\n"
	return s
}
