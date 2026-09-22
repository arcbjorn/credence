import { useEffect, useRef, useState } from "react";
import { Link, useLocation } from "react-router";
import { World } from "../components/World";
import { bps, units } from "../lib/data";
import { readBook } from "../lib/book";
import { bindingPhrase } from "../lib/bindings";
import { borrowerNode, originations, profileNodes, surveyDate, useSurvey, type Projection } from "../lib/observatory";

const ROOM_COPY: Record<string, { title: string; bearing: string; copy: string }> = {
  "/observatory": { title: "Observatory", bearing: "N", copy: "A quiet place to watch capital move." },
  "/atlas": { title: "Atlas", bearing: "E", copy: "Every agent is a coordinate. Every record has weight." },
  "/moons": { title: "Moons", bearing: "S", copy: "The shape of the market. Each book, in its own orbit." },
  "/body": { title: "Retained agent.", bearing: "W", copy: "A personal planet. A record that changes its gravity." },
  "/log": { title: "Log", bearing: "L", copy: "The paths capital has taken. Recorded on chain." },
};

function ArcGlyph({ reverse = false }: { reverse?: boolean }) {
  return <svg viewBox="0 0 80 40" fill="none" aria-hidden="true"><path d="M7 32Q40-20 73 32" stroke="currentColor" strokeWidth="1" /><circle cx={reverse ? 7 : 73} cy="32" r="3" fill="currentColor" /><circle cx={reverse ? 73 : 7} cy="32" r="2" stroke="currentColor" /><path d="M7 36h66" stroke="currentColor" strokeOpacity=".2" /></svg>;
}

export function Observatory() {
  const { pathname } = useLocation();
  const survey = useSurvey();
  const info = ROOM_COPY[pathname] ?? ROOM_COPY["/observatory"]!;
  const isAtlas = pathname === "/atlas";
  const isBody = pathname === "/body";
  const isMoons = pathname === "/moons";
  const isLog = pathname === "/log";
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
    if (showDock) dockClose.current?.focus();
    const close = (event: KeyboardEvent) => { if (event.key === "Escape") { setSelection(null); previousFocus.current?.focus(); } };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [showDock]);

  const replayLoan = (index: number) => { setStep(index); setReplay(v => v + 1); };
  return <section className={`observatory-room ${isBody ? "body-room" : ""} ${showDock ? "room-docked" : ""}`}>
    <div className="room-topline"><span>{info.bearing} / {info.title.toUpperCase().replace(".", "")}</span><span><span className="status-pin" /> {survey.loading ? "READING SURVEY" : "RETAINED EVIDENCE"}</span></div>
    <header className="room-intro"><p className="eyebrow">THE CREDIT BODY / {info.bearing}</p><h1>{info.title}</h1><p>{info.copy}</p>{isBody && <span className="room-source">Development agent · retained run</span>}</header>
    <div className="room-world"><World book={isAtlas ? readBook(null) : book} nodes={nodes} loans={isAtlas || isMoons || isLog ? [] : loans.slice(0, activeIndex + 1)} projection={projection} selected={selection ?? undefined} onSelect={select} replay={replay} paused={paused} /></div>
    {isAtlas && <div className="atlas-tools"><label className="atlas-search"><span aria-hidden="true">⌕</span><input type="search" placeholder="Find a profile" aria-label="Find a profile" value={query} onChange={e => { setQuery(e.target.value); setSelection(null); }} /></label><div className="filter-tabs" role="group" aria-label="Filter profiles">{["all", "approved", "denied"].map(f => <button type="button" key={f} aria-pressed={filter === f} onClick={() => { setFilter(f); setSelection(null); }}>{f === "all" ? "All profiles" : f}</button>)}</div><p className="instrument-note">Comparison scenarios · {profiles.length} coordinates</p>{profiles.length === 0 && !survey.loading && <p>No profiles at this coordinate.</p>}<div className="atlas-profile-list">{profiles.map(p => <button type="button" key={p.id} onClick={() => select(p.id)} aria-pressed={selection === p.id}><span className={`profile-dot ${p.denied ? "profile-dot-hollow" : ""}`} />{p.label}<span>{p.score ?? "—"} ↗</span></button>)}</div></div>}
    {!isAtlas && !isLog && <div className="room-instruments"><span className="instrument-label">THE RETAINED BOOK</span><dl><div><dt>Available capital</dt><dd>{units(book.idle?.toString())}<small> units</small></dd></div><div><dt>Utilization</dt><dd>{book.utilization === null ? "—" : `${(book.utilization * 100).toFixed(1)}%`}</dd></div><div><dt>{isBody ? "Credit limit" : "Principal in flight"}</dt><dd>{units(isBody ? outcome?.capacity?.agent_capacity : book.outstanding?.toString())}</dd></div></dl>{isMoons && <button type="button" className="text-link" onClick={() => select("facility")} disabled={book.idle === null}>Dock with this book ↗</button>}{isBody && <Link className="text-link" to="/agent">Follow the growth ↗</Link>}</div>}
    {isBody && <div className={`body-score ${(outcome?.capacity?.score ?? 0) >= 800 ? "score-exceptional" : ""}`}><svg viewBox="0 0 180 140" role="img" aria-label={`Score ${outcome?.capacity?.score ?? "unavailable"} on the engine's 300–900 scale`}><path d="M26.1 109.26 A68 68 0 1 1 153.9 109.26" fill="none" stroke="var(--border)" strokeWidth="2" pathLength="100" /><path d="M26.1 109.26 A68 68 0 1 1 153.9 109.26" fill="none" stroke="currentColor" strokeWidth="2" pathLength="100" strokeDasharray={`${Math.max(0, Math.min(100, ((outcome?.capacity?.score ?? 300) - 300) / 600 * 100))} 100`} /><text x="90" y="86" textAnchor="middle">{outcome?.capacity?.score ?? "—"}</text><text className="score-label" x="90" y="109" textAnchor="middle">RETAINED SCORE</text></svg><span>300–900 · native engine scale</span></div>}
    {isLog && <div className="log-strip"><p className="instrument-label">INDEXED JOURNEYS</p>{survey.indexed?.loans.length ? survey.indexed.loans.slice(-12).map(l => <button type="button" key={l.id} className="log-journey" onClick={() => select(l.id)}><ArcGlyph reverse={l.status === "Repaid"} /><span>Arc {l.loanId}</span><strong>{l.status}</strong><small>{units(l.principal)} units</small></button>) : <p>{survey.loading ? "Reading the chain log…" : "No indexed journeys available."}</p>}<Link className="text-link" to="/indexed">Inspect the chain log ↗</Link></div>}
    {showDock && <aside className="orbit-dock" aria-labelledby="dock-title"><div className="dock-heading"><span className="instrument-label">DOCKED / {isMoons ? "BOOK" : isLog ? "ARC" : "AGENT"}</span><button ref={dockClose} type="button" aria-label="Close spec sheet" onClick={closeDock}>×</button></div><h2 id="dock-title">{isMoons ? "The facility." : selectedIndexed ? `Arc ${selectedIndexed.loanId}.` : selectedProfile ? `${selectedProfile.name}.` : "Retained agent."}</h2><p className="dock-caption">{isMoons ? "One retained book. Its own boundary." : selectedProfile?.blurb ?? (isLog ? "Reconstructed from chain events." : "History is the source of its gravity.")}</p><dl className="dock-specs">{isMoons ? <><div><dt>Available capital</dt><dd>{units(book.idle?.toString())}</dd></div><div><dt>Outstanding principal</dt><dd>{units(book.outstanding?.toString())}</dd></div><div><dt>Utilization</dt><dd>{book.utilization === null ? "Unmeasured" : `${(book.utilization * 100).toFixed(1)}%`}</dd></div><div><dt>Realized loss</dt><dd>{units(book.loss?.toString())}</dd></div></> : selectedIndexed ? <><div><dt>Status</dt><dd>{selectedIndexed.status}</dd></div><div><dt>Principal</dt><dd>{units(selectedIndexed.principal)}</dd></div><div><dt>Interest collected</dt><dd>{units(selectedIndexed.interestCollected)}</dd></div><div><dt>Principal loss</dt><dd>{units(selectedIndexed.principalLoss)}</dd></div></> : <><div><dt>Score</dt><dd>{selectedOutcome?.capacity?.score ?? "Unmeasured"}</dd></div><div><dt>Credit limit</dt><dd>{units(selectedOutcome?.capacity?.agent_capacity)}</dd></div><div><dt>Bound by</dt><dd>{bindingPhrase(selectedOutcome?.capacity?.agent_binding)}</dd></div><div><dt>Retained APR</dt><dd>{bps(selectedOutcome?.terms?.apr_bps)}</dd></div><div><dt>Tenor</dt><dd>{selectedOutcome?.terms ? `${selectedOutcome.terms.term_seconds / 86400} days` : "—"}</dd></div><div><dt>Decision</dt><dd>{selectedOutcome?.decision === "ApprovedOrigination" ? "Approved" : selectedOutcome?.decision ?? "Unmeasured"}</dd></div></>}</dl><Link className="dock-link" to={isMoons ? "/lender" : isLog ? "/indexed" : isAtlas ? "/compare" : "/assessment"}>Read the full record <span>↗</span></Link><span className="dock-source">{isAtlas ? "Comparison fixture · one policy" : isLog ? "Indexer · chain-derived evidence" : "Retained run · development route"}</span></aside>}
    <div className="room-view-controls" role="group" aria-label="View controls"><button type="button" onClick={() => setProjectionOverride(projection === "atlas" ? "globe" : "atlas")} aria-pressed={projection === "atlas"}>{projection === "atlas" ? "Globe" : "Flatten to atlas"}<span>↗</span></button><button type="button" onClick={() => setPaused(!paused)} aria-pressed={paused}>{paused ? "Resume" : "Pause"}<span>{paused ? "▷" : "Ⅱ"}</span></button>{!isAtlas && !isMoons && !isLog && <button type="button" onClick={() => { setEclipse(!eclipse); setProjectionOverride(null); }} aria-pressed={eclipse}>Eclipse<span>◐</span></button>}</div>
    {!isAtlas && !isMoons && !isLog && loans.length > 0 && <div className="room-timeline"><div className="filmstrip-heading"><span className="instrument-label">RECORDED ARCS</span><Link to="/log">Open the log ↗</Link></div><div className="filmstrip">{loans.slice(-12).map((a, offset) => { const i = Math.max(0, loans.length - 12) + offset; return <button type="button" key={a.assessment_id} aria-label={`Replay origination ${i + 1}`} aria-pressed={activeIndex === i} onClick={() => replayLoan(i)}><ArcGlyph /><span>{String(i + 1).padStart(2, "0")}</span><small>{bps(a.payload.outcome.terms?.apr_bps)}</small></button>; })}<div className="timeline-scrubber"><label htmlFor="survey-time">Survey history <span>{activeIndex + 1} / {loans.length}</span></label><input id="survey-time" type="range" min="0" max={loans.length - 1} value={activeIndex} onChange={e => setStep(Number(e.target.value))} aria-valuetext={`Origination ${activeIndex + 1}, ${bps(outcome?.terms?.apr_bps)} APR`} /><span className="instrument-note">Retained originations · book at latest survey</span></div></div></div>}
    {!survey.loading && !isAtlas && book.idle !== null && book.outstanding !== null && book.idle === 0n && book.outstanding === 0n && <p className="room-empty-state">The core is cold. Be the first light.</p>}
    {!survey.loading && !isAtlas && survey.unavailable.length === 0 && book.idle === null && <p className="room-availability" role="status">This survey retained no book figures.</p>}
    {!survey.loading && survey.unavailable.length > 0 && <p className="room-availability" role="status">Survey incomplete: {survey.unavailable.join(", ")} unavailable.</p>}
    <footer className="room-footer"><span><span className="compass-cross">+</span>{info.title.toUpperCase().replace(".", "")} / {projection.toUpperCase()}</span><span>{isAtlas ? "COMPARISON SCENARIOS" : surveyDate(survey.run) ?? "AWAITING SURVEY"}</span><span className="whisper-hint">HOLD ALT TO HIDE INSTRUMENTS</span></footer>
  </section>;
}
