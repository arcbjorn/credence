import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { FieldGuide, EmptyCoordinate } from "../screens/FieldGuide";
import { Shell } from "../components/Shell";
import { useSurvey } from "./observatory";
import type { Assessment, AssessmentPayload, Comparison, Run } from "./data";

vi.mock("./observatory", async importOriginal => ({
  ...await importOriginal<typeof import("./observatory")>(),
  useSurvey: vi.fn(),
}));

const payload = (score: number, capacity: string, apr: number): AssessmentPayload => ({
  assessment_id: `assessment-${score}`, assessment_time: 1790055601, authorization_class: "Development", input_hash: "input", model_hash: "model", policy_hash: "policy", schema_version: "1", sequence: score, reasons: [], omitted_evidence: [],
  outcome: { decision: "ApprovedOrigination", approved_principal: "4000000000", capacity: { score, agent_capacity: capacity, drawable_capacity: capacity, agent_binding: "HistoryGrowthCap", drawable_binding: "HistoryGrowthCap" }, terms: { apr_bps: apr, approved_principal: "4000000000", term_seconds: 864000, required_equity: "10000000000" } },
});
const assessment = (p: AssessmentPayload): Assessment => ({ assessment_id: p.assessment_id, binding_key: "key", input_hash: p.input_hash, model_hash: p.model_hash, policy_hash: p.policy_hash, assessment_kind: "Origination", decision: p.outcome.decision, risk_permission: null, sequence: p.sequence, assessment_hash: "hash", created_at: p.assessment_time, payload: p });
const run: Run = {
  name: "repay", assessments: [assessment(payload(610, "6200000000", 1200)), assessment(payload(705, "8117138556", 1094))], snapshots: {}, outcomes: [], forecasts: [], chain_events: [],
  checks: [
    { beat: "lender", name: "vault idle cash", detail: "46012860290 base units", passed: true },
    { beat: "lender", name: "vault principal outstanding", detail: "4000000000 base units", passed: true },
    { beat: "lender", name: "vault realized loss", detail: "0 base units", passed: true },
  ],
};
const comparison: Comparison = { policy_hash: "policy", model_hash: "model", profiles: [
  { name: "Unproven", blurb: "A new record.", assessment: payload(610, "6200000000", 1200) },
  { name: "Proven", blurb: "An established record.", assessment: payload(900, "12000000000", 800) },
  { name: "Concentrated", blurb: "A book beyond its limit.", assessment: { ...payload(300, "0", 1200), outcome: { decision: "Denied", capacity: { score: 300, agent_capacity: "0", drawable_capacity: "0", agent_binding: "StressCapacity", drawable_binding: "StressCapacity" } } } },
] };

beforeEach(() => {
  vi.mocked(useSurvey).mockReturnValue({ run, comparison, indexed: { loans: [], events: [] }, loading: false, unavailable: [] });
});
const room = (path: string) => render(<MemoryRouter initialEntries={[path]}><App /></MemoryRouter>);

function LocationReadout() {
  const { pathname, search, hash } = useLocation();
  return <output aria-label="Current route">{pathname}{search}{hash}</output>;
}

describe("dismissing the dock", () => {
  /**
   * The Escape handler was bound whenever the room mounted, not only
   * while a dock was open. With nothing to dismiss it still restored the
   * remembered focus -- so a reader typing in the profile search pressed
   * Escape to clear the field and had focus yanked to whichever node
   * they last selected.
   */
  it("closes an open dock and gives focus back", async () => {
    room("/atlas");
    const list = document.querySelector(".atlas-profile-list") as HTMLElement;
    const trigger = within(list).getByRole("button", { name: /^Proven/ });
    trigger.focus();
    await act(async () => { fireEvent.click(trigger); });
    expect(screen.getByRole("complementary")).toBeInTheDocument();

    await act(async () => { fireEvent.keyDown(window, { key: "Escape" }); });
    expect(screen.queryByRole("complementary")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("leaves focus alone when no dock is open", async () => {
    room("/atlas");
    // Select and dismiss once so a previous focus is remembered. The
    // trigger must really hold focus first: `previousFocus` records
    // `document.activeElement`, and clicking without focusing would
    // remember the body and make the restore invisible.
    const list = document.querySelector(".atlas-profile-list") as HTMLElement;
    const trigger = within(list).getByRole("button", { name: /^Proven/ });
    trigger.focus();
    await act(async () => { fireEvent.click(trigger); });
    await act(async () => { fireEvent.keyDown(window, { key: "Escape" }); });
    expect(document.activeElement).toBe(trigger);

    const search = screen.getByRole("searchbox", { name: /Find a profile/i });
    search.focus();
    await act(async () => { fireEvent.keyDown(window, { key: "Escape" }); });
    expect(document.activeElement).toBe(search);
  });
});

describe("the credit rooms", () => {
  it("keeps old loan links working at one canonical destination", () => {
    render(<MemoryRouter initialEntries={["/lifecycle?loan=2#terms"]}><App /><LocationReadout /></MemoryRouter>);
    expect(screen.getByLabelText("Current route")).toHaveTextContent("/arc?loan=2#terms");
    expect(document.title).toBe("Loan lifecycle — Credence");
  });
  it("enters the observatory from the homepage and keeps the same navigation", () => {
    room("/");
    const before = within(screen.getByRole("navigation", { name: "Primary navigation" })).getAllByRole("link").map(link => link.textContent);
    fireEvent.click(screen.getByRole("link", { name: "See it working" }));
    expect(screen.getByRole("heading", { name: "Observatory", level: 1 })).toBeInTheDocument();
    expect(within(screen.getByRole("navigation", { name: "Primary navigation" })).getAllByRole("link").map(link => link.textContent)).toEqual(before);
  });

  it("changes the projection without changing rooms or the evidence source", () => {
    room("/observatory");
    fireEvent.click(screen.getByRole("button", { name: "Map projection" }));
    expect(screen.getByRole("heading", { name: "Observatory", level: 1 })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Map projection" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Globe projection" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("img", { name: /Credit body, atlas projection/ })).toHaveAccessibleName(/1 profile/);
    fireEvent.click(within(screen.getByRole("navigation", { name: "Primary navigation" })).getByRole("link", { name: "Atlas" }));
    expect(screen.getByRole("heading", { name: "Atlas", level: 1 })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Credit body, atlas projection/ })).toHaveAccessibleName(/3 profiles/);
  });

  it("makes the room's records discoverable and keeps Log inside Observatory", () => {
    room("/observatory");
    fireEvent.click(screen.getByText("Explore records"));
    const records = screen.getByRole("navigation", { name: "Observatory records" });
    fireEvent.click(within(records).getByRole("link", { name: /Log Follow the indexed journeys/ }));
    expect(screen.getByRole("heading", { name: "Log", level: 1 })).toBeInTheDocument();
    expect(within(screen.getByRole("navigation", { name: "Primary navigation" })).getByRole("link", { name: "Observatory" })).toHaveAttribute("aria-current", "location");
    expect(within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByRole("link", { name: "Observatory" })).toHaveAttribute("href", "/observatory");
  });

  it.each([["/observatory", "Observatory"], ["/atlas", "Atlas"], ["/moons", "Moons"], ["/body", "One agent"], ["/log", "Log"]])("renders a usable room at %s", (path, title) => {
    room(path!);
    expect(screen.getByRole("heading", { name: title, level: 1 })).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "View controls" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Credit body/ })).toBeInTheDocument();
  });

  it("filters comparison scenarios and opens the selected profile's real terms", () => {
    room("/atlas");
    fireEvent.click(screen.getByRole("button", { name: "denied" }));
    expect(screen.queryByRole("button", { name: "Proven, score 900" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Concentrated, score 300, denied" }));
    const dock = screen.getByRole("complementary");
    expect(within(dock).getByRole("heading", { name: "Concentrated." })).toBeInTheDocument();
    expect(within(dock).getByText("Denied")).toBeInTheDocument();
    expect(within(dock).getByText("—")).toBeInTheDocument();
    expect(within(dock).queryByText("12.00%")).not.toBeInTheDocument();
  });

  it("searches the atlas and reports no matching coordinates", () => {
    room("/atlas");
    fireEvent.change(screen.getByRole("searchbox", { name: "Find a profile" }), { target: { value: "absent" } });
    expect(screen.getByText("No profiles at this coordinate.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Unproven, score 610" })).not.toBeInTheDocument();
  });

  it("scrubs the borrower history without changing the latest vault measurement", () => {
    room("/body");
    expect(screen.getByRole("img", { name: /Score 705/ })).toBeInTheDocument();
    fireEvent.change(screen.getByRole("slider", { name: /Loan history/ }), { target: { value: "0" } });
    expect(screen.getByRole("img", { name: /Score 610/ })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Credit body/ })).toHaveAccessibleName(/8.0% utilization/);
    expect(screen.getByText("Each loan this agent has taken")).toBeInTheDocument();
  });

  it("dismisses a dock with Escape and restores the opening instrument's focus", () => {
    room("/moons");
    const open = screen.getByRole("button", { name: "Open this pool ↗" });
    act(() => open.focus());
    fireEvent.click(open);
    expect(screen.getByRole("button", { name: "Close spec sheet" })).toHaveFocus();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    expect(open).toHaveFocus();
  });

  it("keeps profile evidence usable when the book is unavailable", () => {
    vi.mocked(useSurvey).mockReturnValue({ run: null, comparison, indexed: null, loading: false, unavailable: ["book", "indexed log"] });
    room("/atlas");
    expect(screen.getByRole("button", { name: "Proven, score 900" })).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("book, indexed log unavailable");
    expect(screen.getByRole("img", { name: /Credit body/ })).toHaveAccessibleName(/Book measurements unavailable/);
  });

  it("changes the guide's chapter and its evidence destination together", () => {
    render(<MemoryRouter><FieldGuide /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "Chapter 3: Core" }));
    expect(screen.getByRole("heading", { name: "The lender's money." })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open the lender's books ↗" })).toHaveAttribute("href", "/lender");
  });

  it("gives an unknown coordinate a way back", () => {
    render(<MemoryRouter><EmptyCoordinate /></MemoryRouter>);
    expect(screen.getByRole("heading", { name: "This coordinate is empty." })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Return to the observatory ↗" })).toHaveAttribute("href", "/observatory");
  });

  it("restores instruments when Alt is released or the window loses focus", () => {
    const { container } = render(<MemoryRouter><Shell><span>body</span></Shell></MemoryRouter>);
    fireEvent.keyDown(window, { key: "Alt" });
    expect(container.querySelector(".shell")).toHaveClass("whisper");
    fireEvent.blur(window);
    expect(container.querySelector(".shell")).not.toHaveClass("whisper");
    fireEvent.keyDown(window, { key: "Alt" });
    fireEvent.keyUp(window, { key: "Alt" });
    expect(container.querySelector(".shell")).not.toHaveClass("whisper");
  });
});
