import { useEffect, useRef, useState } from "react";
import { Link, useLocation } from "react-router";
import { World } from "../components/World";
import { RecordLinks } from "../components/RecordLinks";
import { screenForPath, sectionForScreen } from "../lib/navigation";
import { bps, units } from "../lib/data";
import { readBook } from "../lib/book";
import { bindingPhrase } from "../lib/bindings";
import { borrowerNode, originations, profileNodes, surveyDate, useSurvey, type Projection } from "../lib/observatory";

const ROOM_COPY: Record<string, { title: string; copy: string }> = {
  "/observatory": { title: "Observatory", copy: "Watch the money move. One agent, its loans, and the pool they came from." },
  "/atlas": { title: "Atlas", copy: "Three borrowers, same rules. Click one to see why it was approved or refused." },
  "/moons": { title: "Moons", copy: "The lending pool: how much cash is in it, how much is lent, what it has lost." },
  "/body": { title: "One agent", copy: "A single borrower up close. What it has repaid, and what that earns it." },
  "/log": { title: "Log", copy: "Every loan, rebuilt from public blockchain records rather than our own files." },
};

function ArcGlyph({ reverse = false }: { reverse?: boolean }) {
  return <svg viewBox="0 0 80 40" fill="none" aria-hidden="true"><path d="M7 32Q40-20 73 32" stroke="currentColor" strokeWidth="1" /><circle cx={reverse ? 7 : 73} cy="32" r="3" fill="currentColor" /><circle cx={reverse ? 73 : 7} cy="32" r="2" stroke="currentColor" /><path d="M7 36h66" stroke="currentColor" strokeOpacity=".2" /></svg>;
}

export function Observatory() {
  const { pathname } = useLocation();
  const survey = useSurvey();
  const screen = screenForPath(pathname);
  const section = sectionForScreen(screen);
  const info = ROOM_COPY[screen.path] ?? ROOM_COPY["/observatory"]!;
  const isAtlas = screen.path === "/atlas";
  const isBody = screen.path === "/body";
  const isMoons = screen.path === "/moons";
  const isLog = screen.path === "/log";
  const loans = originations(survey.run);
  const [step, setStep] = useState<number | null>(null);
  const [selection, setSelection] = useState<string | null>(null);
  const [filter, setFilter] = useState("all");
  const [query, setQuery] = useState("");
  const [paused, setPaused] = useState(false);
  const [eclipse, setEclipse] = useState(false);
  const [replay, setReplay] = useState(0);
  const [projectionOverride, setProjectionOverride] = useState<Projection | null>(null);
  const dockClose = useRef<HTMLButtonElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);
  const activeIndex = Math.min(step ?? Math.max(0, loans.length - 1), Math.max(0, loans.length - 1));
  const loan = loans[activeIndex];
  const outcome = loan?.payload.outcome;
  const book = readBook(survey.run);
  const projection: Projection = projectionOverride ?? (isAtlas ? "atlas" : isMoons ? "moons" : eclipse ? "eclipse" : "globe");
  const profiles = profileNodes(survey.comparison).filter(n => (filter === "all" || (filter === "approved" ? !n.denied : n.denied)) && n.label.toLowerCase().includes(query.toLowerCase()));
  const nodes = isAtlas ? profiles : isMoons || isLog ? [] : borrowerNode(loan);
  const selectedProfile = isAtlas ? survey.comparison?.profiles.find(p => p.name === selection) : null;
  const selectedOutcome = selectedProfile?.assessment.outcome ?? outcome;
  const showDock = selection !== null;
  const selectedIndexed = isLog ? survey.indexed?.loans.find(l => l.id === selection) : null;

  const select = (id: string) => { previousFocus.current = document.activeElement as HTMLElement; setSelection(id); };
  const closeDock = () => { setSelection(null); previousFocus.current?.focus(); };
  useEffect(() => {
    setSelection(null); setProjectionOverride(null); setEclipse(false); setStep(null); setFilter("all"); setQuery("");
  }, [pathname]);
  useEffect(() => {
    // Only listen while a dock is open. Bound unconditionally, Escape
    // restored `previousFocus` even with nothing to dismiss -- so a
    // reader typing in the profile search pressed Escape to clear it and
    // had focus yanked to whichever node they had last selected.
    if (!showDock) return;
    dockClose.current?.focus();
    const close = (event: KeyboardEvent) => { if (event.key === "Escape") { setSelection(null); previousFocus.current?.focus(); } };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [showDock]);

  const replayLoan = (index: number) => { setStep(index); setReplay(v => v + 1); };
  return <section className={`observatory-room ${isBody ? "body-room" : ""} ${showDock ? "room-docked" : ""}`}>
    <div className="room-topline">
      {isLog ? <nav className="room-breadcrumb" aria-label="Breadcrumb"><Link to="/observatory">Observatory</Link></nav> : <span>{isAtlas ? "COMPARISON SCENARIOS" : "DEVELOPMENT SURVEY"}</span>}
      {(survey.loading || !isAtlas) && <span>{survey.loading ? "Reading survey…" : isLog ? "Indexed chain events" : surveyDate(survey.run) ?? "Awaiting survey"}</span>}
    </div>
    <header className="room-intro">
      <h1>{info.title}</h1>
      <p>{info.copy}</p>
      {section && <RecordLinks section={section} currentPath={screen.path} compact />}
    </header>
    <div className="room-world"><World book={isAtlas ? readBook(null) : book} nodes={nodes} loans={isAtlas || isMoons || isLog ? [] : loans.slice(0, activeIndex + 1)} projection={projection} selected={selection ?? undefined} onSelect={select} replay={replay} paused={paused} /></div>
    {isAtlas && <div className="atlas-tools"><label className="atlas-search"><input type="search" placeholder="Find a profile" aria-label="Find a profile" value={query} onChange={e => { setQuery(e.target.value); setSelection(null); }} /></label><div className="filter-tabs" role="group" aria-label="Filter profiles">{["all", "approved", "denied"].map(f => <button type="button" key={f} aria-pressed={filter === f} onClick={() => { setFilter(f); setSelection(null); }}>{f === "all" ? "All profiles" : f}</button>)}</div>{profiles.length > 0 && <p className="instrument-note">{profiles.length} borrowers, judged by the same rules</p>}{profiles.length === 0 && !survey.loading && <p>No profiles at this coordinate.</p>}<div className="atlas-profile-list">{profiles.map(p => <button type="button" key={p.id} onClick={() => select(p.id)} aria-pressed={selection === p.id}><span className={`profile-dot ${p.denied ? "profile-dot-hollow" : ""}`} />{p.label}<span>{p.score ?? "—"}</span></button>)}</div></div>}
    {!isAtlas && !isLog && <div className="room-instruments"><span className="instrument-label">THE LENDING POOL</span><dl><div><dt>Cash free to lend</dt><dd>{units(book.idle?.toString())}<small> units</small></dd></div><div><dt>Utilization</dt><dd>{book.utilization === null ? "—" : `${(book.utilization * 100).toFixed(1)}%`}</dd></div><div><dt>{isBody ? "Credit limit" : "Principal in flight"}</dt><dd>{units(isBody ? outcome?.capacity?.agent_capacity : book.outstanding?.toString())}</dd></div></dl>{isMoons && <button type="button" className="text-link" onClick={() => select("facility")} disabled={book.idle === null}>Open this pool</button>}{isBody && <Link className="text-link" to="/agent">See how the limit grew</Link>}</div>}
    {isBody && <div className={`body-score ${(outcome?.capacity?.score ?? 0) >= 800 ? "score-exceptional" : ""}`}><svg viewBox="0 0 180 140" role="img" aria-label={`Score ${outcome?.capacity?.score ?? "unavailable"} on the engine's 300–900 scale`}><path d="M26.1 109.26 A68 68 0 1 1 153.9 109.26" fill="none" stroke="var(--border)" strokeWidth="2" pathLength="100" /><path d="M26.1 109.26 A68 68 0 1 1 153.9 109.26" fill="none" stroke="currentColor" strokeWidth="2" pathLength="100" strokeDasharray={`${Math.max(0, Math.min(100, ((outcome?.capacity?.score ?? 300) - 300) / 600 * 100))} 100`} /><text x="90" y="86" textAnchor="middle">{outcome?.capacity?.score ?? "—"}</text><text className="score-label" x="90" y="109" textAnchor="middle">CREDIT SCORE</text></svg><span>300–900 · native engine scale</span></div>}
    {isLog && <div className="log-strip"><p className="instrument-label">LOANS ON CHAIN</p>{survey.indexed?.loans.length ? survey.indexed.loans.slice(-12).map(l => <button type="button" key={l.id} className="log-journey" onClick={() => select(l.id)}><ArcGlyph reverse={l.status === "Repaid"} /><span>Arc {l.loanId}</span><strong>{l.status}</strong><small>{units(l.principal)} units</small></button>) : <p>{survey.loading ? "Reading the chain log…" : "No indexed journeys available."}</p>}<Link className="text-link" to="/indexed">See the full chain log</Link></div>}
    {showDock && <aside className="orbit-dock" aria-labelledby="dock-title"><div className="dock-heading"><span className="instrument-label">{isMoons ? "BOOK" : isLog ? "ARC" : "AGENT"}</span><button ref={dockClose} type="button" aria-label="Close spec sheet" onClick={closeDock}>Close</button></div><h2 id="dock-title">{isMoons ? "Facility" : selectedIndexed ? `Arc ${selectedIndexed.loanId}` : selectedProfile ? selectedProfile.name : "Retained agent"}</h2><p className="dock-caption">{isMoons ? "One lending pool, with its own cash and its own limits." : selectedProfile?.blurb ?? (isLog ? "Rebuilt from public blockchain records." : "What it repaid is what it can borrow.")}</p><dl className="dock-specs">{isMoons ? <><div><dt>Cash free to lend</dt><dd>{units(book.idle?.toString())}</dd></div><div><dt>Currently lent out</dt><dd>{units(book.outstanding?.toString())}</dd></div><div><dt>Utilization</dt><dd>{book.utilization === null ? "Unmeasured" : `${(book.utilization * 100).toFixed(1)}%`}</dd></div><div><dt>Never paid back</dt><dd>{units(book.loss?.toString())}</dd></div></> : selectedIndexed ? <><div><dt>Status</dt><dd>{selectedIndexed.status}</dd></div><div><dt>Principal</dt><dd>{units(selectedIndexed.principal)}</dd></div><div><dt>Interest earned</dt><dd>{units(selectedIndexed.interestCollected)}</dd></div><div><dt>Never paid back</dt><dd>{units(selectedIndexed.principalLoss)}</dd></div></> : <><div><dt>Score</dt><dd>{selectedOutcome?.capacity?.score ?? "Unmeasured"}</dd></div><div><dt>Credit limit</dt><dd>{units(selectedOutcome?.capacity?.agent_capacity)}</dd></div><div><dt>Limited by</dt><dd>{bindingPhrase(selectedOutcome?.capacity?.agent_binding)}</dd></div><div><dt>Interest rate</dt><dd>{bps(selectedOutcome?.terms?.apr_bps)}</dd></div><div><dt>Loan length</dt><dd>{selectedOutcome?.terms ? `${selectedOutcome.terms.term_seconds / 86400} days` : "—"}</dd></div><div><dt>Decision</dt><dd>{selectedOutcome?.decision === "ApprovedOrigination" ? "Approved" : selectedOutcome?.decision ?? "Unmeasured"}</dd></div></>}</dl><Link className="dock-link" to={isMoons ? "/lender" : isLog ? "/indexed" : isAtlas ? "/compare" : "/assessment"}>See the full record</Link><span className="dock-source">{isAtlas ? "Comparison fixture · one policy" : isLog ? "Indexer · chain-derived evidence" : "Retained run · development route"}</span></aside>}
    <div className="room-view-controls" role="group" aria-label="View controls">
      {!isMoons && <div className="projection-controls" role="group" aria-label="Projection">
        <button type="button" aria-label="Globe projection" aria-pressed={projection === "globe"} onClick={() => { setProjectionOverride("globe"); setEclipse(false); }}>Globe</button>
        <button type="button" aria-label="Map projection" aria-pressed={projection === "atlas"} onClick={() => { setProjectionOverride("atlas"); setEclipse(false); }}>Map</button>
      </div>}
      <button type="button" onClick={() => setPaused(!paused)} aria-pressed={paused}>{paused ? "Resume rotation" : "Pause rotation"}</button>
      {!isAtlas && !isMoons && !isLog && <button type="button" onClick={() => { setEclipse(!eclipse); setProjectionOverride(null); }} aria-pressed={eclipse}>Eclipse</button>}
    </div>
    {!isAtlas && !isMoons && !isLog && loans.length > 0 && <div className="room-timeline"><div className="filmstrip-heading"><span className="instrument-label">LOANS SO FAR</span><Link to="/log">Open the log</Link></div><div className="filmstrip">{loans.slice(-12).map((a, offset) => { const i = Math.max(0, loans.length - 12) + offset; return <button type="button" key={a.assessment_id} aria-label={`Replay origination ${i + 1}`} aria-pressed={activeIndex === i} onClick={() => replayLoan(i)}><ArcGlyph /><span>{String(i + 1).padStart(2, "0")}</span><small>{bps(a.payload.outcome.terms?.apr_bps)}</small></button>; })}<div className="timeline-scrubber"><label htmlFor="survey-time">Loan history <span>{activeIndex + 1} / {loans.length}</span></label><input id="survey-time" type="range" min="0" max={loans.length - 1} value={activeIndex} onChange={e => setStep(Number(e.target.value))} aria-valuetext={`Origination ${activeIndex + 1}, ${bps(outcome?.terms?.apr_bps)} APR`} /><span className="instrument-note">Each loan this agent has taken</span></div></div></div>}
    {!survey.loading && !isAtlas && book.idle !== null && book.outstanding !== null && book.idle === 0n && book.outstanding === 0n && <p className="room-empty-state">The core is cold. Be the first light.</p>}
    {!survey.loading && !isAtlas && survey.unavailable.length === 0 && book.idle === null && <p className="room-availability" role="status">This survey retained no book figures.</p>}
    {!survey.loading && survey.unavailable.length > 0 && <p className="room-availability" role="status">Survey incomplete: {survey.unavailable.join(", ")} unavailable.</p>}
  </section>;
}
