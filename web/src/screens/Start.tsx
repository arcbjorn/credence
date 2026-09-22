import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { units, bps } from "../lib/data";
import { readBook } from "../lib/book";
import { CLAIMS, EARNED, GROUPS, REFUSED, UNEARNED, type Claim } from "../lib/claims";
import { borrowerNode, originations, surveyDate, useSurvey, type Projection } from "../lib/observatory";
import { World } from "../components/World";
import { BrandMark } from "../components/Shell";
import { Deployment } from "../components/Deployment";

const CHAPTERS: { title: string; label: string; copy: string; projection: Projection; note: string }[] = [
  { title: "Agents can't borrow.", label: "A new coordinate for capital", copy: "No salary, no address, no credit file — so nobody will lend to them. We price them on the one thing they have: a record of what they actually did.", projection: "globe", note: "Start with the problem." },
  { title: "Every agent is a point.", label: "01 / The shell", copy: "One point per agent, sized by what we'd lend it today. Repay on time and it grows. Nothing here is hand-set.", projection: "globe", note: "Size is the limit. History sets the size." },
  { title: "Every loan is an arc.", label: "02 / The passage", copy: "Money leaves the lender, reaches the agent, comes back. A longer loan draws a higher arc, and each one sets the next limit.", projection: "globe", note: "Borrow, repay, borrow more." },
  { title: "The lender's cash.", label: "03 / The core", copy: "The bright centre is cash waiting to be lent. Lend it out and the centre dims. Every loan is drawn from here.", projection: "core", note: "Bright core means money to lend." },
  { title: "Four layers, one body.", label: "04 / The section", copy: "Identity, then track record, then collateral held, then the lender's cash. A loan passes through all four, and each leaves evidence.", projection: "layers", note: "Every layer keeps a receipt." },
  { title: "Each pool, its own moon.", label: "05 / The moons", copy: "Each pool keeps its own cash and borrowers, so one going bad can't drain the rest. There is exactly one today.", projection: "moons", note: "One pool today. Not pretending otherwise." },
  { title: "How much is lent out.", label: "06 / The eclipse", copy: "Light is cash available, shadow is cash already lent. The overlap is how much of the pool is working.", projection: "eclipse", note: "Overlap is how much is at work." },
  { title: "Flatten it into a map.", label: "07 / The atlas", copy: "Same agents, same numbers, laid out flat. Easier to see why one borrower got denied.", projection: "atlas", note: "Same data, easier to scan." },
  { title: "Go look for yourself.", label: "08 / The observatory", copy: "Pick an agent, open its record, re-run the decision in your browser. If a number is missing, the page says so.", projection: "globe", note: "Check it rather than trust it." },
];

export function ClaimRow({ claim }: { claim: Claim }) {
  return <details className={`claim claim-${claim.status}`}><summary><span className="claim-title">{claim.title}</span><span className="claim-status">{claim.status === "earned" ? "live" : "building"}</span></summary><div className="claim-body"><p className="claim-detail">{claim.detail}</p><p className="claim-support"><span className="claim-support-label">Runs on</span>{claim.support}</p>{claim.to && <Link to={claim.to} className="claim-link">Inspect the record ↗</Link>}</div></details>;
}

export function Start() {
  const navigate = useNavigate();
  const survey = useSurvey();
  const story = useRef<HTMLDivElement>(null);
  const [chapter, setChapter] = useState(0);
  const [paused, setPaused] = useState(false);
  const [replay, setReplay] = useState(0);
  const [progress, setProgress] = useState(0);
  const loans = originations(survey.run);
  const last = loans.at(-1);
  const book = readBook(survey.run);
  const current = CHAPTERS[chapter]!;

  useEffect(() => {
    if (chapter === 2) setReplay(value => value + 1);
  }, [chapter]);

  useEffect(() => {
    let frame = 0;
    const update = () => {
      frame = 0;
      const el = story.current;
      if (!el) return;
      const extent = el.offsetHeight - window.innerHeight;
      const p = Math.max(0, Math.min(1, -el.getBoundingClientRect().top / Math.max(1, extent)));
      setProgress(p);
      setChapter(Math.min(8, Math.floor(p * 9)));
    };
    const scroll = () => { if (!frame) frame = requestAnimationFrame(update); };
    update(); window.addEventListener("scroll", scroll, { passive: true }); window.addEventListener("resize", scroll);
    return () => { cancelAnimationFrame(frame); window.removeEventListener("scroll", scroll); window.removeEventListener("resize", scroll); };
  }, []);

  const goToChapter = (i: number) => {
    if (!story.current) return;
    const top = window.scrollY + story.current.getBoundingClientRect().top;
    window.scrollTo({ top: top + (story.current.offsetHeight - window.innerHeight) * (i / 9 + (i === 0 ? 0 : 0.015)), behavior: "instant" });
  };

  return <>
    <div className="planet-story" ref={story}>
      <section className={`story-viewport story-chapter-${chapter}`} aria-label="The credit body, an interactive field guide">
        <div className="story-topline"><span>THE AGENT CREDIT PROTOCOL</span><span>AN OPEN FIELD OF POSSIBILITY <span className="tiny-cross">+</span></span></div>
        <div className="split-left story-copy">
          <p className="eyebrow"><span className="status-pin" />{current.label}</p>
          <h1 className="hero" key={current.title}>{current.title}</h1>
          <div className="hero-objects story-world">
            <World book={book} nodes={borrowerNode(last)} loans={loans} projection={current.projection} paused={paused} replay={replay} onSelect={id => navigate(id === "facility" ? "/moons" : "/body")} />
          </div>
          <div className="satellite satellite-top"><span className="satellite-line" /><div><span className="instrument-label">FIG. {String(chapter + 1).padStart(2, "0")}</span><p>{current.note}</p></div></div>
          <div className="satellite satellite-bottom"><span className="instrument-label">ATMOSPHERE</span><span>{book.utilization === null ? "Awaiting measurement" : `${((1 - book.utilization) * 100).toFixed(1)}% headroom`}</span><span className="satellite-line" /></div>
          <p className="hero-lede">{current.copy}</p>
          <Link to={chapter === 7 ? "/atlas" : "/observatory"} className="start-enter">{chapter === 7 ? "Compare the borrowers" : "See it working"} <span aria-hidden="true">↗</span></Link>
          <p className="hero-access">Every number came from a real run.</p>
        </div>
        <div className="world-controls"><button type="button" onClick={() => setPaused(!paused)} aria-pressed={paused}>{paused ? "Resume rotation" : "Pause rotation"}<span aria-hidden="true">{paused ? "▷" : "Ⅱ"}</span></button>{loans.length > 0 && <button type="button" onClick={() => setReplay(v => v + 1)}>Replay a loan <span>↗</span></button>}</div>
        <div className="story-readings">
          <Link to="/lender"><span className="instrument-label">01 / CASH FREE TO LEND</span><strong>{book.idle === null ? "—" : units(book.idle.toString())}<small> units</small></strong></Link>
          <Link to="/agent"><span className="instrument-label">02 / LATEST INTEREST RATE</span><strong>{bps(last?.payload.outcome.terms?.apr_bps)}</strong></Link>
          <Link to="/arc"><span className="instrument-label">03 / LOANS MADE</span><strong>{survey.run ? String(loans.length).padStart(2, "0") : "—"}<small> loans</small></strong></Link>
          <div className="reading-provenance"><span className="status-pin" /><span>{survey.loading ? "Loading" : survey.run ? "Real run, saved" : "No data loaded"}<small>{surveyDate(survey.run) ?? "Run the demo to see the numbers."}</small></span></div>
        </div>
        <div className="story-bottomline"><button type="button" onClick={() => goToChapter(chapter === 8 ? 0 : chapter + 1)}>{chapter === 8 ? "BACK TO THE START ↑" : "KEEP SCROLLING ↓"}</button><div className="chapter-steps" role="group" aria-label="Sections">{CHAPTERS.map((c, i) => <button type="button" key={c.label} aria-label={c.label} aria-pressed={chapter === i} onClick={() => goToChapter(i)}><span /></button>)}</div><span>{String(chapter + 1).padStart(2, "0")} <span className="muted">/ 09</span></span></div>
        <div className="story-progress" style={{ transform: `scaleX(${progress})` }} />
      </section>
    </div>
    <section className="survey-proof">
      <div className="proof-intro"><p className="eyebrow">09 / WEATHER THAT HAPPENED</p><h2>A record.<br /><em>Not a promise.</em></h2><p>Every figure has a source. Every decision leaves a trace. Open the records and see what this body can prove.</p><Deployment /><Link to="/replay" className="text-link">Replay a decision ↗</Link></div>
      <div className="ledger">
        <header className="ledger-head"><h3>The field record</h3><p><strong>{EARNED} of {EARNED + UNEARNED} work today.</strong></p></header>
        {GROUPS.map(g => {
          const rows = CLAIMS.filter(c => c.group === g.key);
          if (rows.length === 0) return null;
          return <section className="claim-group" key={g.key}>
            <div className="claim-group-intro"><h4>{g.title}</h4><p>{g.blurb}</p></div>
            <div className="claim-group-rows">{rows.map(c => <ClaimRow key={c.title} claim={c} />)}</div>
          </section>;
        })}
        <details className="refused"><summary>What this does not claim</summary><ul>{REFUSED.map(r => <li key={r}>{r}</li>)}</ul><p>Determinism buys auditability, not accuracy. Replaying a decision proves what the model did.</p></details>
      </div>
    </section>
    <footer className="landing-footer"><div className="footer-invitation"><span className="eyebrow">THERE IS MORE TO THIS WORLD.</span><Link to="/observatory">See it working.<span aria-hidden="true">↗</span></Link></div><div className="footer-baseline"><Link className="footer-brand" to="/"><BrandMark />credence.</Link><span>Credit has a memory.</span><Link to="/guide">Read the field guide ↗</Link></div></footer>
  </>;
}
