-- Credence gateway persistence (docs/04).
--
-- The Go gateway is the only writer. Event-derived projections can be
-- rebuilt; source evidence and assessment snapshots must be retained,
-- because a hash without retrievable evidence is insufficient for replay.

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- Identities and the bindings financing attaches to.
CREATE TABLE IF NOT EXISTS agents (
    agent_key                  TEXT PRIMARY KEY, -- registry_chain:registry:agent_id
    identity_registry_chain_id INTEGER NOT NULL,
    identity_registry          TEXT    NOT NULL,
    agent_id                   TEXT    NOT NULL,
    created_at                 INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS bindings (
    binding_key    TEXT PRIMARY KEY,
    agent_key      TEXT    NOT NULL REFERENCES agents(agent_key),
    operator       TEXT    NOT NULL,
    credit_account TEXT    NOT NULL,
    facility_id    TEXT    NOT NULL,
    config_version INTEGER NOT NULL,
    config_hash    TEXT    NOT NULL,
    policy_epoch   INTEGER NOT NULL,
    policy_hash    TEXT    NOT NULL,
    created_at     INTEGER NOT NULL,
    -- Block from which this binding's outcomes have been indexed. Zero
    -- means the gateway cannot prove it enumerated anything, so history
    -- coverage is incomplete rather than verified-empty (docs/04).
    indexed_from   INTEGER NOT NULL DEFAULT 0
);

-- Immutable observations. Origin is a property of the evidence itself:
-- an acquisition failure changes availability, never origin (docs/04).
CREATE TABLE IF NOT EXISTS observations (
    evidence_id  TEXT PRIMARY KEY,
    source       TEXT    NOT NULL,
    origin       TEXT    NOT NULL CHECK (origin IN ('Observed','Synthetic','PolicyAssumption')),
    state        TEXT    NOT NULL CHECK (state IN ('Present','Missing','Stale','Conflicting','Unsupported')),
    coverage     TEXT    NOT NULL CHECK (coverage IN ('Complete','Partial','Unknown')),
    observed_at  INTEGER,
    retrieved_at INTEGER NOT NULL,
    chain_id     INTEGER,
    block_number INTEGER,
    block_hash   TEXT,
    scenario_id  TEXT,
    content_hash TEXT    NOT NULL,
    payload      BLOB    NOT NULL
);

-- Derived-feature lineage. A numeric contribution alone cannot explain the
-- scope of the data beneath it, so parents are kept explicit.
CREATE TABLE IF NOT EXISTS evidence_edges (
    child_evidence_id  TEXT NOT NULL REFERENCES observations(evidence_id),
    parent_evidence_id TEXT NOT NULL REFERENCES observations(evidence_id),
    method_version     TEXT NOT NULL,
    PRIMARY KEY (child_evidence_id, parent_evidence_id)
);

-- Exact canonical bytes, not a row that could later be re-serialized
-- differently (docs/04).
CREATE TABLE IF NOT EXISTS snapshots (
    input_hash   TEXT PRIMARY KEY,
    binding_key  TEXT    NOT NULL REFERENCES bindings(binding_key),
    schema_version TEXT  NOT NULL,
    created_at   INTEGER NOT NULL,
    payload      BLOB    NOT NULL
);

CREATE TABLE IF NOT EXISTS assessments (
    assessment_id   TEXT PRIMARY KEY,
    binding_key     TEXT    NOT NULL REFERENCES bindings(binding_key),
    input_hash      TEXT    NOT NULL REFERENCES snapshots(input_hash),
    model_hash      TEXT    NOT NULL,
    policy_hash     TEXT    NOT NULL,
    assessment_kind TEXT    NOT NULL CHECK (assessment_kind IN ('Origination','Monitoring')),
    decision        TEXT    NOT NULL CHECK (decision IN ('Approved','Denied','Unavailable')),
    risk_permission TEXT    NOT NULL CHECK (risk_permission IN ('Allowed','Blocked')),
    sequence        INTEGER NOT NULL,
    assessment_hash TEXT    NOT NULL,
    created_at      INTEGER NOT NULL,
    payload         BLOB    NOT NULL,
    -- Set when the chain accepted this assessment. Retention happens
    -- BEFORE publication, so an evaluation that never landed must not
    -- reserve its proposed sequence: the next attempt would collide and
    -- retries could never advance (docs/04 retains every decision,
    -- docs/05 admits one accepted sequence per binding).
    accepted_at     INTEGER
);

-- One ACCEPTED sequence per binding across both kinds (docs/05).
-- Partial, so unpublished attempts are retained without reserving it.
CREATE UNIQUE INDEX IF NOT EXISTS idx_assessments_accepted_sequence
    ON assessments(binding_key, sequence) WHERE accepted_at IS NOT NULL;

-- Pinned artifacts. Model and policy are hashed independently.
CREATE TABLE IF NOT EXISTS models (
    model_hash TEXT PRIMARY KEY,
    model_id   TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    params     BLOB    NOT NULL
);

CREATE TABLE IF NOT EXISTS policies (
    policy_hash TEXT PRIMARY KEY,
    policy_id   TEXT    NOT NULL,
    epoch       INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    params      BLOB    NOT NULL
);

-- Commitments made before disbursement, and their append-only resolutions.
CREATE TABLE IF NOT EXISTS intents (
    intent_hash TEXT PRIMARY KEY,
    binding_key TEXT    NOT NULL REFERENCES bindings(binding_key),
    request_id  TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    payload     BLOB    NOT NULL
);

CREATE TABLE IF NOT EXISTS forecasts (
    forecast_id     TEXT PRIMARY KEY,
    binding_key     TEXT    NOT NULL REFERENCES bindings(binding_key),
    intent_hash     TEXT REFERENCES intents(intent_hash),
    event_definition TEXT   NOT NULL,
    probability_ppm INTEGER NOT NULL,
    horizon_seconds INTEGER NOT NULL,
    committed_at    INTEGER NOT NULL,
    payload         BLOB    NOT NULL
);

CREATE TABLE IF NOT EXISTS outcomes (
    outcome_id  TEXT PRIMARY KEY,
    forecast_id TEXT    NOT NULL REFERENCES forecasts(forecast_id),
    resolution  TEXT    NOT NULL CHECK (resolution IN ('Occurred','DidNotOccur','NotObservable')),
    resolved_at INTEGER NOT NULL,
    brier_ppm   INTEGER,
    payload     BLOB    NOT NULL
);

-- Chain events, indexed by the triple that identifies a log uniquely.
CREATE TABLE IF NOT EXISTS chain_events (
    chain_id    INTEGER NOT NULL,
    tx_hash     TEXT    NOT NULL,
    log_index   INTEGER NOT NULL,
    block_number INTEGER NOT NULL,
    block_hash  TEXT    NOT NULL,
    contract    TEXT    NOT NULL,
    event_name  TEXT    NOT NULL,
    payload     BLOB    NOT NULL,
    -- Set when a reorg invalidates the block. The fact that a decision was
    -- made is preserved rather than deleted (docs/04).
    invalidated_at INTEGER,
    PRIMARY KEY (chain_id, tx_hash, log_index)
);

CREATE TABLE IF NOT EXISTS chain_cursors (
    chain_id      INTEGER NOT NULL,
    contract      TEXT    NOT NULL,
    block_number  INTEGER NOT NULL,
    block_hash    TEXT    NOT NULL,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (chain_id, contract)
);

-- Exactly-once economic effects come from request ids and contract nonces;
-- network delivery itself is not exactly once (docs/04).
-- Deliberately no foreign key on binding_key. This is an audit trail of
-- what was attempted, and it must remain writable regardless of whether the
-- binding is registered yet. Coupling delivery to referential state would
-- let a missing row block a publish, or make a historical attempt
-- unrecordable once the reconciliation trail is what matters most.
CREATE TABLE IF NOT EXISTS transactions (
    idempotency_key TEXT PRIMARY KEY,
    binding_key     TEXT NOT NULL,
    kind            TEXT    NOT NULL,
    -- Superseded is distinct from Failed: a newer sequence won the race,
    -- so this delivery can never be accepted and must not be retried.
    status          TEXT    NOT NULL CHECK (status IN ('Pending','Confirmed','Failed','Superseded')),
    tx_hash         TEXT,
    sequence        INTEGER,
    nonce           INTEGER,
    attempts        INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    detail          TEXT    NOT NULL DEFAULT '',
    payload         BLOB    NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
    job_id      TEXT PRIMARY KEY,
    kind        TEXT    NOT NULL,
    run_after   INTEGER NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL CHECK (status IN ('Queued','Running','Done','Failed')),
    payload     BLOB    NOT NULL
);

-- Virtual-clock runs. A fixture pack is immutable once recorded (docs/07).
CREATE TABLE IF NOT EXISTS scenario_runs (
    run_id            TEXT PRIMARY KEY,
    generator_version TEXT    NOT NULL,
    seed              INTEGER NOT NULL,
    model_hash        TEXT    NOT NULL,
    policy_hash       TEXT    NOT NULL,
    started_at        INTEGER NOT NULL,
    payload           BLOB    NOT NULL
);

CREATE TABLE IF NOT EXISTS scenario_events (
    run_id       TEXT    NOT NULL REFERENCES scenario_runs(run_id),
    sequence     INTEGER NOT NULL,
    virtual_time INTEGER NOT NULL,
    event_id     TEXT    NOT NULL,
    event_kind   TEXT    NOT NULL,
    payload      BLOB    NOT NULL,
    PRIMARY KEY (run_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_assessments_binding ON assessments(binding_key, sequence DESC);
CREATE INDEX IF NOT EXISTS idx_events_block ON chain_events(chain_id, block_number);
CREATE INDEX IF NOT EXISTS idx_jobs_ready ON jobs(status, run_after);
CREATE INDEX IF NOT EXISTS idx_scenario_time ON scenario_events(run_id, virtual_time, sequence);
