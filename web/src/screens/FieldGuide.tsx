import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { World } from "../components/World";
import { readBook } from "../lib/book";
import { borrowerNode, originations, useSurvey, type Projection } from "../lib/observatory";

const CHAPTERS: { title: string; subtitle: string; copy: string; projection: Projection; to: string; link: string }[] = [
  { title: "Survey", subtitle: "Where the numbers come from.", copy: "Every figure came out of a real run and was saved. Development runs, comparison scenarios and chain events are labelled separately, because they are not the same kind of evidence. A missing measurement stays missing — the gap does not get filled with something plausible.", projection: "globe", to: "/evidence", link: "Inspect sources" },
  { title: "Shell", subtitle: "An agent is a point.", copy: "Each point is one borrower. How big it is, is how much we'd lend it. A hollow point was refused. Put three borrowers side by side under the same rules and you can see exactly what changed the answer.", projection: "globe", to: "/compare", link: "Compare three borrowers" },
  { title: "Core", subtitle: "The lender's money.", copy: "The bright centre is idle cash. Four figures stay separate: cash free to lend, principal out on loan, interest collected, principal written off. Only the first one can be withdrawn. Mixing them is how lending dashboards flatter themselves.", projection: "core", to: "/lender", link: "Open the lender's books" },
  { title: "Weather", subtitle: "How much is already lent.", copy: "Utilization is the share of the pool that's out on loan. Lend more and the glow around the body thins — less cash left, less room for a bad surprise. When the numbers are missing, nothing is drawn. An empty pool is not a safe one.", projection: "eclipse", to: "/perpl", link: "See market risk" },
  { title: "Moons", subtitle: "One pool, one orbit.", copy: "Each pool holds its own cash and its own borrowers. A pool going bad cannot drain the others. Open a moon for its cash, what it lent and what it lost. One pool exists today.", projection: "moons", to: "/moons", link: "Open the pool" },
  { title: "Scars", subtitle: "Losses stay where they happened.", copy: "A default lands on the lender that made the loan, not on the next borrower. It stays in that lender's records next to the chain evidence. Recovered principal plus written-off principal has to equal what went out.", projection: "layers", to: "/lender", link: "Inspect realized loss" },
];

export function FieldGuide() {
  const navigate = useNavigate();
  const survey = useSurvey();
  const loans = originations(survey.run);
  const [chapter, setChapter] = useState(0);
  const current = CHAPTERS[chapter]!;
  return <section className="field-guide"><header><h1>Field guide</h1><p>The visual language this site uses, and the numbers underneath it.</p></header><div className="guide-layout"><nav aria-label="Field guide chapters">{CHAPTERS.map((c, i) => <button key={c.title} type="button" aria-label={`Chapter ${i + 1}: ${c.title}`} aria-pressed={chapter === i} onClick={() => setChapter(i)}><span>{String(i + 1).padStart(2, "0")}</span>{c.title}</button>)}</nav><article><h2>{current.subtitle}</h2><p>{current.copy}</p><Link className="text-link" to={current.to}>{current.link}</Link><div className="guide-footnote">Numbers come from a retained development run. Surface texture and point positions are decorative.</div></article><div className="guide-world"><World book={readBook(survey.run)} nodes={borrowerNode(loans.at(-1))} loans={loans} projection={current.projection} labels={false} onSelect={id => navigate(id === "facility" ? "/moons" : "/body")} /></div></div></section>;
}

export function EmptyCoordinate() {
  return <section className="empty-coordinate"><div><p className="eyebrow">404</p><h1>This coordinate is empty.</h1><Link to="/observatory" className="text-link">Return to the observatory</Link></div><World book={readBook(null)} nodes={[]} paused /></section>;
}
