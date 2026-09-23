/** One map for the header, page titles, breadcrumbs and record links. */
export type SectionId = "observatory" | "atlas" | "moons" | "body" | "guide";

export interface Section {
  id: SectionId;
  path: string;
  label: string;
  records: string[];
}

export const SECTIONS: Section[] = [
  { id: "observatory", path: "/observatory", label: "Observatory", records: ["/log", "/arc", "/indexed"] },
  { id: "atlas", path: "/atlas", label: "Atlas", records: ["/compare"] },
  { id: "moons", path: "/moons", label: "Moons", records: ["/borrow", "/lender", "/live", "/perpl"] },
  { id: "body", path: "/body", label: "Body", records: ["/agent", "/earned", "/assessment", "/enforcement"] },
  { id: "guide", path: "/guide", label: "Field guide", records: ["/evidence", "/replay"] },
];

/** Log is a frequent destination, even though its records belong to Observatory. */
export const PRIMARY_LINKS = [
  ...SECTIONS.slice(0, 4),
  { path: "/log", label: "Log" },
  ...SECTIONS.slice(4),
];

export interface Screen {
  path: string;
  title: string;
  layout: "landing" | "world" | "document" | "empty";
  section?: SectionId;
  description?: string;
}

export const SCREENS: Screen[] = [
  { path: "/", title: "The credit body", layout: "landing" },
  { path: "/observatory", title: "Observatory", layout: "world", section: "observatory" },
  { path: "/atlas", title: "Atlas", layout: "world", section: "atlas" },
  { path: "/moons", title: "Moons", layout: "world", section: "moons" },
  { path: "/body", title: "Body", layout: "world", section: "body" },
  { path: "/guide", title: "Field guide", layout: "document", section: "guide" },
  { path: "/log", title: "Log", layout: "world", section: "observatory", description: "Follow the indexed journeys." },
  { path: "/arc", title: "Loan lifecycle", layout: "document", section: "observatory", description: "Inspect origination, terms and forecasts." },
  { path: "/indexed", title: "Indexed lifecycle", layout: "document", section: "observatory", description: "Read what the chain alone records." },
  { path: "/compare", title: "Three agents", layout: "document", section: "atlas", description: "Compare profiles under one policy." },
  { path: "/borrow", title: "Borrow desk", layout: "document", section: "moons", description: "Ask the engine for terms yourself." },
  { path: "/lender", title: "Lender", layout: "document", section: "moons", description: "Separate available cash, exposure and loss." },
  { path: "/live", title: "Live on Monad", layout: "document", section: "moons", description: "Read the deployment as it stands now." },
  { path: "/perpl", title: "Perpl market risk", layout: "document", section: "moons", description: "Inspect the observed market and its stress." },
  { path: "/agent", title: "Agent overview", layout: "document", section: "body", description: "Follow the agent's capacity and rate." },
  { path: "/earned", title: "What a record earns", layout: "document", section: "body", description: "Watch a rate fall on record alone." },
  { path: "/assessment", title: "Assessment detail", layout: "document", section: "body", description: "See what bounded a credit decision." },
  { path: "/enforcement", title: "Enforcement", layout: "document", section: "body", description: "Inspect the account's on-chain limits." },
  { path: "/evidence", title: "Evidence", layout: "document", section: "guide", description: "Trace each input to its source." },
  { path: "/replay", title: "Replay", layout: "document", section: "guide", description: "Reproduce a decision with the same engine." },
];

const EMPTY: Screen = { path: "", title: "This coordinate is empty", layout: "empty" };

/** Match the router's trailing-slash/case behavior without inventing routes. */
export function screenForPath(pathname: string): Screen {
  const path = pathname.replace(/\/+$/, "").toLowerCase() || "/";
  return SCREENS.find(screen => screen.path === (path === "/lifecycle" ? "/arc" : path)) ?? EMPTY;
}

export function sectionForScreen(screen: Screen): Section | undefined {
  return SECTIONS.find(section => section.id === screen.section);
}

export function recordsForSection(section: Section): Screen[] {
  return section.records.flatMap(path => {
    const screen = SCREENS.find(item => item.path === path);
    return screen ? [screen] : [];
  });
}
