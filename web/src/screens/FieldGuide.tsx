import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { World } from "../components/World";
import { readBook } from "../lib/book";
import { borrowerNode, originations, useSurvey, type Projection } from "../lib/observatory";

const CHAPTERS: { title: string; subtitle: string; copy: string; projection: Projection; to: string; link: string }[] = [
  { title: "Survey", subtitle: "Where the numbers come from.", copy: "Every figure on this site was produced by a real run and saved. No placeholders, no rounded-up demos. If a measurement is missing, the page says so rather than filling the gap with something plausible. Each record links back to its source.", projection: "globe", to: "/evidence", link: "See the sources" },
  { title: "Shell", subtitle: "An agent is a point.", copy: "Each point is one borrower. How big it is, is how much we'd lend it. A hollow point was refused. Put three borrowers side by side under the same rules and you can see exactly what changed the answer.", projection: "globe", to: "/compare", link: "Compare three borrowers" },
  { title: "Core", subtitle: "The lender's money.", copy: "The bright middle is cash sitting idle, ready to lend. Four numbers are kept apart on purpose: cash free to lend, cash currently lent out, interest earned, and money never paid back. Mixing them is how lending dashboards flatter themselves.", projection: "core", to: "/lender", link: "Open the lender's books" },
  { title: "Weather", subtitle: "How much is already lent.", copy: "Utilization is the share of the pool that's out on loan. Lend more and the glow around the body thins — less cash left, less room for a bad surprise. When the numbers are missing, nothing is drawn. An empty pool is not a safe one.", projection: "eclipse", to: "/perpl", link: "See market risk" },
  { title: "Moons", subtitle: "One pool, one orbit.", copy: "Each lending pool keeps its own cash and its own borrowers, so trouble in one can't drain another. Open a moon to see its cash, what's lent, and what it lost. There is exactly one today — more when there's real evidence behind them.", projection: "moons", to: "/moons", link: "Open the pool" },
  { title: "Scars", subtitle: "Losses stay where they happened.", copy: "When a loan isn't paid back, the lender wears that loss — not the next borrower who repaid on time. It stays visible in the lender's records next to the chain evidence for it. We tried drawing it on the borrower once. It was wrong, and we took it off.", projection: "layers", to: "/lender", link: "See a real loss" },
];

export function FieldGuide() {
  const navigate = useNavigate();
  const survey = useSurvey();
  const loans = originations(survey.run);
  const [chapter, setChapter] = useState(0);
  const current = CHAPTERS[chapter]!;
  return <section className="field-guide"><header><p className="eyebrow">A PLAIN GUIDE TO AGENT CREDIT</p><h1>How it works.</h1><p>Six ideas. Read them in order and the rest of the site makes sense.</p></header><div className="guide-layout"><nav aria-label="Field guide chapters">{CHAPTERS.map((c, i) => <button key={c.title} type="button" aria-label={`Chapter ${i + 1}: ${c.title}`} aria-pressed={chapter === i} onClick={() => setChapter(i)}><span>{String(i + 1).padStart(2, "0")}</span>{c.title}<span>↗</span></button>)}</nav><article><span className="instrument-label">CHAPTER {String(chapter + 1).padStart(2, "0")}</span><h2>{current.subtitle}</h2><p>{current.copy}</p><Link className="text-link" to={current.to}>{current.link} ↗</Link><div className="guide-footnote">Numbers come from a real development run. The surface texture and where points sit are decorative.</div></article><div className="guide-world"><World book={readBook(survey.run)} nodes={borrowerNode(loans.at(-1))} loans={loans} projection={current.projection} labels={false} onSelect={id => navigate(id === "facility" ? "/moons" : "/body")} /></div></div></section>;
}

export function EmptyCoordinate() {
  return <section className="empty-coordinate"><div><p className="eyebrow">404 / OUTSIDE THE SURVEY</p><h1>This coordinate is empty.</h1><p>The map continues elsewhere.</p><Link to="/observatory" className="text-link">Return to the observatory ↗</Link></div><World book={readBook(null)} nodes={[]} paused /></section>;
}
