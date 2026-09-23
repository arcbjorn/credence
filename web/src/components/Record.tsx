import { useLocation } from "react-router";
import { screenForPath, sectionForScreen } from "../lib/navigation";
import { RecordTabs } from "./RecordLinks";

/**
 * The shared frame for a field record.
 *
 * Ten screens opened with the same two elements -- a `record-title` and a
 * `lede` -- written out sixteen times. Each copy was free to drift, and
 * several had: different heading levels, different spacing beneath, a
 * `<p>` in one place and a `<div>` in another. A reader moving between
 * records saw the page shift under them.
 *
 * The spacing lives here rather than in each screen, so changing the gap
 * between a record's title and its body is one edit, not sixteen.
 */

export function RecordHeader({
  title,
  children,
}: {
  title: string;
  /** The lede: one or two sentences on what this record shows. */
  children?: React.ReactNode;
}) {
  const { pathname } = useLocation();
  const screen = screenForPath(pathname);
  const section = sectionForScreen(screen);
  return (
    <div className="record-header page-header">
      <h1 className="record-title page-title">{title}</h1>
      {children !== undefined && <p className="lede">{children}</p>}
      {section && screen.layout === "document" && <RecordTabs section={section} currentPath={screen.path} />}
    </div>
  );
}
