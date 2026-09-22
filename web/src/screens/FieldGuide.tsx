import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { World } from "../components/World";
import { readBook } from "../lib/book";
import { borrowerNode, originations, useSurvey, type Projection } from "../lib/observatory";

const CHAPTERS: { title: string; subtitle: string; copy: string; projection: Projection; to: string; link: string }[] = [
  { title: "Survey", subtitle: "Begin with what is known.", copy: "This observatory reads retained evidence. Each amount, decision and limit comes from an assessment or a recorded chain check. A missing measurement stays missing. Open a field record to inspect its source.", projection: "globe", to: "/evidence", link: "Inspect the evidence" },
  { title: "Shell", subtitle: "An agent earns its place.", copy: "A point holds an identity and a record. Its size follows its credit limit. A hollow point marks a refused profile. The Atlas places comparison scenarios side by side under one policy, so you can see what changes a decision.", projection: "globe", to: "/compare", link: "Compare the profiles" },
  { title: "Core", subtitle: "Capital is the light inside.", copy: "Available lender cash lights the core. Principal in flight is exposure, not available cash. Collected interest and realized loss are separate records. Open the book and keep those four facts apart.", projection: "core", to: "/lender", link: "Read the lender's book" },
  { title: "Weather", subtitle: "The atmosphere carries the risk.", copy: "Atmosphere is one minus utilization. As more of the book is lent, the rim becomes thinner. The eclipse offers a second reading of the same fraction. Missing book data draws no reassuring atmosphere.", projection: "eclipse", to: "/perpl", link: "Survey market risk" },
  { title: "Moons", subtitle: "A boundary around a book.", copy: "A moon is a facility in orbit. Dock with it to inspect its available capital, outstanding principal and realized loss. This release carries one retained facility. Additional markets belong here when their evidence exists.", projection: "moons", to: "/moons", link: "Dock with the facility" },
  { title: "Scars", subtitle: "The record remembers.", copy: "A loss belongs to the book that suffered it. It must never be painted onto a borrower that repaid. Realized loss remains visible in the lender's records, alongside the chain evidence that accounts for it.", projection: "layers", to: "/lender", link: "Inspect realized loss" },
];

export function FieldGuide() {
  const navigate = useNavigate();
  const survey = useSurvey();
  const loans = originations(survey.run);
  const [chapter, setChapter] = useState(0);
  const current = CHAPTERS[chapter]!;
  return <section className="field-guide"><header><p className="eyebrow">A FIELD GUIDE TO AGENT CREDIT</p><h1>Learn the physics.</h1><p>One body. A few laws. Everything follows.</p></header><div className="guide-layout"><nav aria-label="Field guide chapters">{CHAPTERS.map((c, i) => <button key={c.title} type="button" aria-label={`Chapter ${i + 1}: ${c.title}`} aria-pressed={chapter === i} onClick={() => setChapter(i)}><span>{String(i + 1).padStart(2, "0")}</span>{c.title}<span>↗</span></button>)}</nav><article><span className="instrument-label">CHAPTER {String(chapter + 1).padStart(2, "0")}</span><h2>{current.subtitle}</h2><p>{current.copy}</p><Link className="text-link" to={current.to}>{current.link} ↗</Link><div className="guide-footnote">Survey source: retained development run. Surface texture and chart coordinates are illustrative.</div></article><div className="guide-world"><World book={readBook(survey.run)} nodes={borrowerNode(loans.at(-1))} loans={loans} projection={current.projection} labels={false} onSelect={id => navigate(id === "facility" ? "/moons" : "/body")} /></div></div></section>;
}

export function EmptyCoordinate() {
  return <section className="empty-coordinate"><div><p className="eyebrow">404 / OUTSIDE THE SURVEY</p><h1>This coordinate is empty.</h1><p>The map continues elsewhere.</p><Link to="/observatory" className="text-link">Return to the observatory ↗</Link></div><World book={readBook(null)} nodes={[]} paused /></section>;
}
