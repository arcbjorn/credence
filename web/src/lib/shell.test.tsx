import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { Shell } from "../components/Shell";

const renderShell = (path = "/") => render(
  <MemoryRouter initialEntries={[path]}>
    <Shell><h1>Screen content</h1></Shell>
  </MemoryRouter>,
);
const primary = () => screen.getByRole("navigation", { name: "Primary navigation" });
const destinations = () => within(primary()).getAllByRole("link").map(link => ({
  label: link.textContent, to: link.getAttribute("href"),
}));
const HEADER = [
  { label: "Observatory", to: "/observatory" },
  { label: "Atlas", to: "/atlas" },
  { label: "Moons", to: "/moons" },
  { label: "Body", to: "/body" },
  { label: "Field guide", to: "/guide" },
];

describe("one header throughout the site", () => {
  it.each(["/", "/observatory", "/atlas", "/moons", "/body", "/log", "/guide", "/assessment", "/lender", "/replay", "/unknown"])("keeps the same destinations at %s", path => {
    renderShell(path);
    expect(destinations()).toEqual(HEADER);
    expect(screen.getAllByRole("banner")).toHaveLength(1);
    expect(screen.queryByRole("navigation", { name: "Field records" })).not.toBeInTheDocument();
    expect(within(screen.getByRole("banner")).getAllByRole("link")).toHaveLength(6);
  });

  it("does not change the header when the homepage scrolls", () => {
    renderShell();
    const header = screen.getByRole("banner");
    fireEvent.scroll(window, { target: { scrollY: 400 } });
    expect(screen.getByRole("banner")).toBe(header);
    expect(header).toHaveClass("bar");
    expect(header).not.toHaveClass("bar-quiet", "bar-away");
    expect(destinations()).toEqual(HEADER);
  });

  it.each([
    ["/agent", "Body", "Agent overview"],
    ["/assessment", "Body", "Assessment detail"],
    ["/enforcement", "Body", "Enforcement"],
    ["/compare", "Atlas", "Three agents"],
    ["/lender", "Moons", "Lender"],
    ["/perpl", "Moons", "Perpl market risk"],
    ["/arc", "Observatory", "Loan lifecycle"],
    ["/lifecycle", "Observatory", "Loan lifecycle"],
    ["/indexed", "Observatory", "Indexed lifecycle"],
    ["/evidence", "Field guide", "Evidence"],
    ["/replay", "Field guide", "Replay"],
  ])("gives %s a parent room and a breadcrumb back", (path, parent, title) => {
    renderShell(path);
    const parentLink = within(primary()).getByRole("link", { name: parent });
    expect(parentLink).toHaveAttribute("aria-current", "location");
    const breadcrumb = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(breadcrumb).getByRole("link", { name: parent })).toHaveAttribute("href", parentLink.getAttribute("href"));
    expect(within(breadcrumb).getByText(title!)).toHaveAttribute("aria-current", "page");
    expect(document.title).toBe(`${title} — Credence`);
  });

  it("offers related records from the current room only", () => {
    renderShell("/assessment");
    const related = screen.getByRole("navigation", { name: "Body records" });
    expect(within(related).getAllByRole("link").map(link => link.getAttribute("href"))).toEqual(["/agent", "/enforcement"]);
    expect(within(related).queryByRole("link", { name: /Lender/ })).not.toBeInTheDocument();
  });

  it("keeps the field guide available in the mobile menu and closes after navigation", () => {
    renderShell("/body");
    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    expect(screen.getByRole("button", { name: "Close navigation" })).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(within(primary()).getByRole("link", { name: "Field guide" }));
    expect(screen.getByRole("button", { name: "Open navigation" })).toHaveAttribute("aria-expanded", "false");
    expect(within(primary()).getByRole("link", { name: "Field guide" })).toHaveAttribute("aria-current", "page");
    expect(document.title).toBe("Field guide — Credence");
    expect(screen.getByRole("main")).toHaveFocus();
  });

  it("returns focus to the menu button on Escape", () => {
    renderShell("/atlas");
    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.getByRole("button", { name: "Open navigation" })).toHaveFocus();
  });

  it("matches existing routes with trailing slashes without fabricating a nested room", () => {
    const view = renderShell("/Atlas/");
    expect(within(primary()).getByRole("link", { name: "Atlas" })).toHaveAttribute("aria-current", "page");
    expect(document.title).toBe("Atlas — Credence");
    view.unmount();
    renderShell("/observatory/extra");
    expect(document.title).toBe("This coordinate is empty — Credence");
    expect(primary().querySelector("[aria-current]")).toBeNull();
    expect(screen.queryByRole("navigation", { name: "Breadcrumb" })).not.toBeInTheDocument();
  });
});
