import { describe, expect, it } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import type { Assessment, Cohort, Comparison } from "./data";
import { cohortNodes, loanArcs, populationNodes, profileNodes } from "./observatory";

/**
 * The globe's agent population.
 *
 * The coordinates were three hardcoded pairs indexed `i % 3`, which meant
 * a fourth agent landed exactly on the first. The set has since grown to
 * eight, so the failure would have been silent and visible only as a
 * globe that looked sparser than the data.
 */
const comparisonOf = (names: string[]): Comparison => ({
  policy_hash: "p",
  model_hash: "m",
  profiles: names.map((name) => ({
    name,
    blurb: "",
    assessment: {
      assessment_id: name, assessment_time: 0, authorization_class: "Development",
      input_hash: "i", model_hash: "m", policy_hash: "p", schema_version: "1",
      sequence: 1, reasons: [], omitted_evidence: [],
      outcome: {
        decision: "ApprovedOrigination",
        capacity: { agent_capacity: "1000", drawable_capacity: "1000", agent_binding: "X", drawable_binding: "X", score: 500 },
      },
    },
  })),
});

describe("arranging agents on the globe", () => {
  it("gives every agent its own position", () => {
    const names = ["a", "b", "c", "d", "e", "f", "g", "h"];
    const nodes = profileNodes(comparisonOf(names));
    expect(nodes).toHaveLength(names.length);
    const seen = new Set(nodes.map((n) => `${n.latitude},${n.longitude}`));
    expect(seen.size).toBe(names.length);
  });

  it("keeps every position on the sphere", () => {
    for (const n of profileNodes(comparisonOf(["a", "b", "c", "d", "e"]))) {
      expect(n.latitude).toBeGreaterThanOrEqual(-90);
      expect(n.latitude).toBeLessThanOrEqual(90);
      expect(n.longitude).toBeGreaterThanOrEqual(-180);
      expect(n.longitude).toBeLessThanOrEqual(180);
    }
  });

  it("puts nothing exactly at a pole, where longitude collapses", () => {
    for (const count of [1, 2, 3, 8, 20]) {
      const names = Array.from({ length: count }, (_, i) => `a${i}`);
      for (const n of profileNodes(comparisonOf(names))) {
        expect(Math.abs(n.latitude)).toBeLessThan(90);
      }
    }
  });

  it("handles an empty comparison without inventing agents", () => {
    expect(profileNodes(null)).toEqual([]);
    expect(profileNodes(comparisonOf([]))).toEqual([]);
  });

  it("leads the population with the retained borrower", () => {
    // The one agent with real chain history keeps its own fixed
    // position rather than being shuffled in by a lattice that knows
    // nothing about provenance.
    const loan = {
      input_hash: "i", sequence: 1,
      payload: { outcome: { capacity: { agent_capacity: "42", score: 700 } } },
    } as unknown as Assessment;
    const nodes = populationNodes(loan, comparisonOf(["a", "b"]));
    expect(nodes).toHaveLength(3);
    expect(nodes[0]!.id).toBe("borrower");
    expect(nodes[0]!.capacity).toBe("42");
  });

  it("renders the population even with no retained loan", () => {
    expect(populationNodes(undefined, comparisonOf(["a", "b"]))).toHaveLength(2);
  });
});

/**
 * Against the committed fixture, not a mock: the globe renders what
 * `emit_comparison` wrote, and a profile the engine declined to evaluate
 * has no capacity to size a node by.
 */
const PATH = "../fixtures/demo/comparison.json";
describe.skipIf(!existsSync(PATH))("the shipped profiles", () => {
  const load = (): Comparison => JSON.parse(readFileSync(PATH, "utf8")) as Comparison;

  it("puts every shipped profile on the globe", () => {
    const comparison = load();
    expect(profileNodes(comparison)).toHaveLength(comparison.profiles.length);
    expect(comparison.profiles.length).toBeGreaterThanOrEqual(5);
  });

  it("survives a profile the engine declined to evaluate", () => {
    // `Unavailable` carries no capacity block at all. Sizing it at zero
    // is honest -- there is nothing to size it by -- but a crash or a
    // NaN radius is not.
    for (const node of profileNodes(load())) {
      expect(node.capacity).toMatch(/^\d+$/);
      expect(Number.isNaN(Number(node.capacity))).toBe(false);
    }
  });

  it("marks anything not approved as denied", () => {
    const comparison = load();
    const nodes = profileNodes(comparison);
    comparison.profiles.forEach((profile, i) => {
      const approved = profile.assessment.outcome.decision === "ApprovedOrigination";
      expect(nodes[i]!.denied).toBe(!approved);
    });
  });
});

const cohortOf = (names: string[]): Cohort => ({
  agents: names.map((name, i) => ({
    name,
    account: `0x${String(i).repeat(40).slice(0, 40)}`,
    assessments: [
      {
        assessment_id: name, binding_key: "b", input_hash: "i", model_hash: "m",
        policy_hash: "p", assessment_kind: "Origination", decision: "ApprovedOrigination",
        risk_permission: null, sequence: 1, assessment_hash: "h", created_at: 0,
        payload: {
          assessment_id: name, assessment_time: 0, authorization_class: "Development",
          input_hash: "i", model_hash: "m", policy_hash: "p", schema_version: "1",
          sequence: 1, reasons: [], omitted_evidence: [],
          outcome: {
            decision: "ApprovedOrigination",
            capacity: { agent_capacity: String(1000 * (i + 1)), drawable_capacity: "1000", agent_binding: "X", drawable_binding: "X", score: 600 + i },
          },
        },
      },
    ],
    snapshots: {},
  })),
});

describe("the cohort on the globe", () => {
  it("renders one node per agent that borrowed", () => {
    expect(cohortNodes(cohortOf(["sable", "ember", "cinder"]))).toHaveLength(3);
  });

  it("sizes an agent by its LAST assessment", () => {
    // Capacity is what the record has earned by the end of the run, not
    // what it started with.
    const cohort = cohortOf(["sable"]);
    cohort.agents[0]!.assessments.push({
      ...cohort.agents[0]!.assessments[0]!,
      payload: {
        ...cohort.agents[0]!.assessments[0]!.payload,
        outcome: {
          decision: "ApprovedOrigination",
          capacity: { agent_capacity: "9999", drawable_capacity: "9999", agent_binding: "X", drawable_binding: "X", score: 800 },
        },
      },
    });
    expect(cohortNodes(cohort)[0]!.capacity).toBe("9999");
  });

  it("reports no agents when the demo has not run", () => {
    expect(cohortNodes(null)).toEqual([]);
  });

  it("puts the real borrowers ahead of the assessed profiles", () => {
    // Provenance ordering: the retained borrower first, then agents that
    // borrowed on chain, then scenarios that never touched one.
    const nodes = populationNodes(
      { input_hash: "i", sequence: 1, payload: { outcome: { capacity: { agent_capacity: "1", score: 1 } } } } as unknown as Assessment,
      comparisonOf(["Unproven", "Proven"]),
      cohortOf(["sable", "ember"]),
    );
    expect(nodes.map(n => n.id)).toEqual(["borrower", "sable", "ember", "Unproven", "Proven"]);
  });

  it("gives the whole population distinct positions", () => {
    // Two lattices laid independently would overlap, landing cohort
    // agents exactly on profiles. The set is laid out once, together.
    const nodes = populationNodes(
      undefined,
      comparisonOf(["a", "b", "c", "d", "e", "f", "g", "h"]),
      // Names distinct from the profiles above: a shared id is dropped by
      // design, and reusing one here would test dedup rather than layout.
      cohortOf(["s1", "s2", "s3", "s4"]),
    );
    expect(nodes).toHaveLength(12);
    const seen = new Set(nodes.map(n => `${n.latitude},${n.longitude}`));
    expect(seen.size).toBe(nodes.length);
  });

  it("keeps the retained borrower at a stable position", () => {
    // Another screen links to this node; it must not move as the cohort
    // grows or it becomes unfindable.
    const loan = { input_hash: "i", sequence: 1, payload: { outcome: { capacity: { agent_capacity: "1", score: 1 } } } } as unknown as Assessment;
    const small = populationNodes(loan, comparisonOf(["a"]), cohortOf(["s"]));
    const large = populationNodes(loan, comparisonOf(["a", "b", "c"]), cohortOf(["s", "e", "c"]));
    expect(small[0]!.latitude).toBe(large[0]!.latitude);
    expect(small[0]!.longitude).toBe(large[0]!.longitude);
  });

  it("never renders one id twice", () => {
    // A name shared between the cohort and the profiles would otherwise
    // produce two nodes fighting over the same focusable button.
    const nodes = populationNodes(undefined, comparisonOf(["sable"]), cohortOf(["sable"]));
    expect(nodes).toHaveLength(1);
  });
});

describe("attributing loans to the agent that took them", () => {
  const origination = (principal: string, term = 864000) => ({
    assessment_id: `a-${principal}-${term}`, binding_key: "b", input_hash: "i",
    model_hash: "m", policy_hash: "p", assessment_kind: "Origination",
    decision: "ApprovedOrigination", risk_permission: null, sequence: 1,
    assessment_hash: "h", created_at: 0,
    payload: {
      assessment_id: "a", assessment_time: 0, authorization_class: "Development",
      input_hash: "i", model_hash: "m", policy_hash: "p", schema_version: "1",
      sequence: 1, reasons: [], omitted_evidence: [],
      outcome: {
        decision: "ApprovedOrigination", approved_principal: principal,
        terms: { apr_bps: 1200, approved_principal: principal, term_seconds: term, required_equity: "0" },
      },
    },
  });

  const runOf = (principals: string[]) => ({
    name: "repay", assessments: principals.map(p => origination(p)),
    snapshots: {}, outcomes: [], forecasts: [], chain_events: [],
  }) as unknown as Parameters<typeof loanArcs>[0];

  it("routes the retained run's loans to the borrower node", () => {
    const arcs = loanArcs(runOf(["1", "2", "3"]));
    expect(arcs).toHaveLength(3);
    for (const arc of arcs) expect(arc.agent).toBe("borrower");
  });

  it("gives each cohort agent its own arcs", () => {
    // The bug this pins: every arc used to terminate at nodes[0], so a
    // whole cohort's traffic converged on one point and drew over itself.
    const cohort = cohortOf(["sable", "ember"]);
    const arcs = loanArcs(null, cohort);
    expect(arcs.map(a => a.agent).sort()).toEqual(["ember", "sable"]);
  });

  it("draws the retained run and the cohort together", () => {
    const arcs = loanArcs(runOf(["1", "2"]), cohortOf(["sable"]));
    expect(arcs).toHaveLength(3);
    expect(new Set(arcs.map(a => a.agent))).toEqual(new Set(["borrower", "sable"]));
  });

  it("never draws a refusal as a transfer", () => {
    // A denied assessment moved no money. Drawing an arc for it would
    // show a payment the chain never made.
    const denied = origination("5") as Record<string, unknown>;
    (denied["payload"] as { outcome: { decision: string } }).outcome.decision = "Denied";
    const run = {
      name: "repay", assessments: [origination("1"), denied],
      snapshots: {}, outcomes: [], forecasts: [], chain_events: [],
    } as unknown as Parameters<typeof loanArcs>[0];
    expect(loanArcs(run)).toHaveLength(1);
  });

  it("carries the principal and tenor each arc is drawn from", () => {
    const [arc] = loanArcs(runOf(["7500"]));
    expect(arc!.principal).toBe("7500");
    expect(arc!.term_seconds).toBe(864000);
  });

  it("reports nothing when neither source has run", () => {
    expect(loanArcs(null, null)).toEqual([]);
  });
});
