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
  { title: "The credit body.", label: "A new coordinate for capital", copy: "A living record of what an agent can borrow. Built from what it has done. Bounded by what its book can bear.", projection: "globe", note: "Capital has a shape." },
  { title: "Every agent is a coordinate.", label: "01 / The shell", copy: "Identity gives an agent a place. Its record gives that place weight. A stronger history opens a larger facility.", projection: "globe", note: "A point. A record. A reputation." },
  { title: "Every loan is an arc.", label: "02 / The passage", copy: "Capital leaves the lender and reaches the agent. A longer term draws a higher arc. Every journey leaves a record.", projection: "globe", note: "The path of a promise." },
  { title: "Liquidity lives in the core.", label: "03 / The core", copy: "Available capital lights the centre. As the book lends, its atmosphere thins. When capital returns, the body breathes again.", projection: "core", note: "Read the book from the light." },
  { title: "A market with a core.", label: "04 / The section", copy: "Identity. Reputation. Escrow. Settlement. Four layers, one credit body. Each has a job. Each leaves evidence.", projection: "layers", note: "Nothing hidden beneath the surface." },
  { title: "One book. Its own weather.", label: "05 / The moons", copy: "A facility has its own limits and its own exposure. Survey the retained book here. More moons arrive when more markets do.", projection: "moons", note: "Isolation begins with a boundary." },
  { title: "Lenders cast the light.", label: "06 / The eclipse", copy: "Agents cast the shadow. The overlap is utilization: capital at work, measured against capital available.", projection: "eclipse", note: "Risk, in plain sight." },
  { title: "Read the book as a map.", label: "07 / The atlas", copy: "Unroll the body. Compare its coordinates. The same evidence, seen from another angle.", projection: "atlas", note: "A different projection. The same truth." },
  { title: "Find your coordinate.", label: "08 / The observatory", copy: "Follow the capital. Inspect an agent. Replay the evidence. The body is open for observation.", projection: "globe", note: "The observatory is open." },
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
          <Link to={chapter === 7 ? "/atlas" : "/observatory"} className="start-enter">{chapter === 7 ? "Explore the atlas" : "Enter the observatory"} <span aria-hidden="true">↗</span></Link>
          <p className="hero-access">Open to observe. Built to be verified.</p>
        </div>
        <div className="world-controls"><button type="button" onClick={() => setPaused(!paused)} aria-pressed={paused}>{paused ? "Resume rotation" : "Pause rotation"}<span aria-hidden="true">{paused ? "▷" : "Ⅱ"}</span></button>{loans.length > 0 && <button type="button" onClick={() => setReplay(v => v + 1)}>Replay an arc <span>↗</span></button>}</div>
        <div className="story-readings">
          <Link to="/lender"><span className="instrument-label">01 / AVAILABLE CAPITAL</span><strong>{book.idle === null ? "—" : units(book.idle.toString())}<small> units</small></strong></Link>
          <Link to="/agent"><span className="instrument-label">02 / LATEST RETAINED APR</span><strong>{bps(last?.payload.outcome.terms?.apr_bps)}</strong></Link>
          <Link to="/arc"><span className="instrument-label">03 / RECORDED ORIGINATIONS</span><strong>{survey.run ? String(loans.length).padStart(2, "0") : "—"}<small> arcs</small></strong></Link>
          <div className="reading-provenance"><span className="status-pin" /><span>{survey.loading ? "Reading the survey" : survey.run ? "Retained survey" : "Survey unavailable"}<small>{surveyDate(survey.run) ?? "The core awaits a measurement."}</small></span></div>
        </div>
        <div className="story-bottomline"><button type="button" onClick={() => goToChapter(chapter === 8 ? 0 : chapter + 1)}>{chapter === 8 ? "BACK TO THE SHELL ↑" : "SCROLL TO SURVEY ↓"}</button><div className="chapter-steps" role="group" aria-label="Story chapters">{CHAPTERS.map((c, i) => <button type="button" key={c.label} aria-label={c.label} aria-pressed={chapter === i} onClick={() => goToChapter(i)}><span /></button>)}</div><span>{String(chapter + 1).padStart(2, "0")} <span className="muted">/ 09</span></span></div>
        <div className="story-progress" style={{ transform: `scaleX(${progress})` }} />
      </section>
    </div>
    <section className="survey-proof">
      <div className="proof-intro"><p className="eyebrow">09 / WEATHER THAT HAPPENED</p><h2>A record.<br /><em>Not a promise.</em></h2><p>Every figure has a source. Every decision leaves a trace. Open the records and see what this body can prove.</p><Deployment /><Link to="/replay" className="text-link">Replay a decision ↗</Link></div>
      <div><header className="ledger-head"><h3>The field record</h3><p><strong>{EARNED} of {EARNED + UNEARNED} work today.</strong></p></header><div className="ledger">{CLAIMS.map(c => <ClaimRow key={c.title} claim={c} />)}</div><details className="refused"><summary>What this does not claim</summary><ul>{REFUSED.map(r => <li key={r}>{r}</li>)}</ul><p>Determinism buys auditability, not accuracy. Replaying a decision proves what the model did.</p></details></div>
    </section>
    <footer className="landing-footer"><div className="footer-invitation"><span className="eyebrow">THERE IS MORE TO THIS WORLD.</span><Link to="/observatory">Enter the observatory.<span aria-hidden="true">↗</span></Link></div><div className="footer-baseline"><Link className="footer-brand" to="/"><BrandMark />credence.</Link><span>Credit has a memory.</span><Link to="/guide">Read the field guide ↗</Link></div></footer>
  </>;
}
