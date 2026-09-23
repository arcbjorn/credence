import { Link } from "react-router";
import { recordsForSection, screenForPath, type Section } from "../lib/navigation";

export function LinkArrow() {
  return <svg className="link-arrow" viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M4 10h12m-5-5 5 5-5 5" stroke="currentColor" strokeWidth="1.2" /></svg>;
}

/** A room's own records, separate from the fixed navigation between rooms. */
export function RecordLinks({ section, currentPath, compact = false }: {
  section: Section;
  currentPath?: string;
  compact?: boolean;
}) {
  const records = recordsForSection(section).filter(record => record.path !== currentPath);
  if (!records.length) return null;

  return (
    <nav className={compact ? "room-records" : "related-records"} aria-label={`${section.label} records`}>
      <p className="eyebrow">{compact ? "Explore the records" : `More in ${section.label}`}</p>
      <div>
        {records.map(record => (
          <Link className="record-link" key={record.path} to={record.path}>
            <span>{record.title}<LinkArrow /></span>{" "}
            <small>{record.description}</small>
          </Link>
        ))}
      </div>
    </nav>
  );
}

/** Sibling records stay visible before the document, including its current location. */
export function RecordTabs({ section, currentPath }: { section: Section; currentPath: string }) {
  return <nav className="record-tabs" aria-label={`${section.label} pages`}>
    {recordsForSection(section).map(record => <Link key={record.path} to={record.path} aria-current={record.path === currentPath ? "page" : undefined}>{record.title}</Link>)}
  </nav>;
}

/**
 * The six a first-time reader should reach first.
 *
 * Ordered so the two they can OPERATE come first. `/borrow` runs the
 * real engine on a request they compose and `/earned` is the project's
 * central claim as one controlled run; both answer "what does this
 * actually do" faster than any page that only reports. `/live` follows
 * because a reader who has seen the model wants to know whether it is
 * deployed, and the three retained records close.
 */
const FEATURED_RECORDS = [
  "/borrow",
  "/earned",
  "/live",
  "/log",
  "/compare",
  "/replay",
].map(screenForPath);

export function ExploreRecords() {
  return <section className="explore-records" aria-labelledby="explore-records-title">
    <header><p className="eyebrow">Follow the evidence</p><h2 id="explore-records-title">A closer look.</h2><p>Ask the engine for terms, watch a rate fall on record alone, or read the chain as it stands.</p></header>
    <nav aria-label="Explore data pages">
      {FEATURED_RECORDS.map(record => <Link className="record-link" key={record.path} to={record.path}><span>{record.title}<LinkArrow /></span><small>{record.description}</small></Link>)}
    </nav>
  </section>;
}
