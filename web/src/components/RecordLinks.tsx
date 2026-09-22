import { Link } from "react-router";
import { recordsForSection, type Section } from "../lib/navigation";

/** A room's own records, separate from the fixed navigation between rooms. */
export function RecordLinks({ section, currentPath, compact = false }: {
  section: Section;
  currentPath?: string;
  compact?: boolean;
}) {
  const records = recordsForSection(section).filter(record => record.path !== currentPath);
  if (!records.length) return null;

  if (compact) {
    return (
      <details className="room-records" key={`${section.id}-${currentPath}`}>
        <summary>Explore records <span aria-hidden="true">+</span></summary>
        <nav aria-label={`${section.label} records`}>
          {records.map(record => (
            <Link key={record.path} to={record.path}>
              <span>{record.title}</span>{" "}
              <small>{record.description}</small>
            </Link>
          ))}
        </nav>
      </details>
    );
  }

  return (
    <nav className="related-records" aria-label={`${section.label} records`}>
      <p className="eyebrow">{currentPath ? `More in ${section.label}` : `${section.label} records`}</p>
      <div>
        {records.map(record => (
          <Link key={record.path} to={record.path}>
            <span>{record.title} <span aria-hidden="true">↗</span></span>{" "}
            <small>{record.description}</small>
          </Link>
        ))}
      </div>
    </nav>
  );
}
