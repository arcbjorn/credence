import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { units, bps } from "../lib/data";
import { readBook } from "../lib/book";
import { CLAIMS, EARNED, REFUSED, UNEARNED, type Claim } from "../lib/claims";
import { borrowerNode, originations, surveyDate, useSurvey, type Projection } from "../lib/observatory";
import { World } from "../components/World";
import { BrandMark } from "../components/Shell";
import { Deployment } from "../components/Deployment";

const CHAPTERS: { title: string; label: string; copy: string; projection: Projection; note: string }[] = [
  { title: "Agents can't borrow money.", label: "A new coordinate for capital", copy: "They trade, they pay for compute, they hold positions. But no salary, no address, no credit file. So nobody will lend to them. We price them on the one thing they do have: a record of what they actually did.", projection: "globe", note: "Start with the problem." },
  { title: "Every agent is a point.", label: "01 / The shell", copy: "One point per agent. Its size is the credit limit we'd give it today. That limit comes from its history — repay on time and it grows, get denied and it doesn't. Nothing here is hand-set.", projection: "globe", note: "Size is the limit. History sets the size." },
  { title: "Every loan is an arc.", label: "02 / The passage", copy: "Money leaves the lender, reaches the agent, comes back. A longer loan draws a higher arc. Each completed arc becomes part of the record that sets the next limit.", projection: "globe", note: "Borrow, repay, borrow more." },
  { title: "The lender's cash is the core.", label: "03 / The core", copy: "The bright centre is cash sitting idle, ready to lend. Lend it out and the centre dims. This is the lender's side of the book — the pool every loan is drawn from.", projection: "core", note: "Bright core means money to lend." },
  { title: "Four layers, one body.", label: "04 / The section", copy: "Cut it open and you get identity, then the track record, then collateral held in escrow, then the lender's cash at the centre. A loan passes through all four. Each one leaves evidence you can check.", projection: "layers", note: "Every layer keeps a receipt." },
  { title: "Each market is its own moon.", label: "05 / The moons", copy: "A lending pool has its own cash, its own borrowers, its own risk. One pool going bad shouldn't take the others with it — so each gets its own orbit. Today there's exactly one. More when there's real evidence for them.", projection: "moons", note: "One pool today. Not pretending otherwise." },
  { title: "How much is lent out right now.", label: "06 / The eclipse", copy: "The light is cash available. The shadow is cash already lent. Where they overlap is utilization — the share of the pool that's working. A lot of overlap means little left to lend.", projection: "eclipse", note: "Overlap is how much is at work." },
  { title: "Flatten it into a map.", label: "07 / The atlas", copy: "Same agents, same numbers, laid out flat instead of wrapped on a sphere. Easier to compare three borrowers side by side and see why one got denied.", projection: "atlas", note: "Same data, easier to scan." },
  { title: "Go look for yourself.", label: "08 / The observatory", copy: "Every number here was produced by a real run and kept. Pick an agent, open its record, re-run the decision in your browser. If something is missing, the page says so instead of guessing.", projection: "globe", note: "Check it rather than trust it." },
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
          <p className="hero-access">Nothing here is a mockup. Every number came from a real run.</p>
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
      <div><header className="ledger-head"><h3>The field record</h3><p><strong>{EARNED} of {EARNED + UNEARNED} work today.</strong></p></header><div className="ledger">{CLAIMS.map(c => <ClaimRow key={c.title} claim={c} />)}</div><details className="refused"><summary>What this does not claim</summary><ul>{REFUSED.map(r => <li key={r}>{r}</li>)}</ul><p>Determinism buys auditability, not accuracy. Replaying a decision proves what the model did.</p></details></div>
    </section>
    <footer className="landing-footer"><div className="footer-invitation"><span className="eyebrow">THERE IS MORE TO THIS WORLD.</span><Link to="/observatory">See it working.<span aria-hidden="true">↗</span></Link></div><div className="footer-baseline"><Link className="footer-brand" to="/"><BrandMark />credence.</Link><span>Credit has a memory.</span><Link to="/guide">Read the field guide ↗</Link></div></footer>
  </>;
}
