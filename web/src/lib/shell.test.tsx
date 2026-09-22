import { act, render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { Shell } from "../components/Shell";

/**
 * The landing bar is transparent at the top so the page opens on the
 * claim rather than on chrome. That is only safe AT the top: a
 * transparent fixed bar with content scrolling underneath renders the
 * navigation and the page text on top of each other, which is what it
 * did until 22 September.
 *
 * The second test here covers the subtler half. State cannot be seeded
 * from `useState` alone, because a reload restores the previous scroll
 * offset after React mounts -- so the component syncs once on mount
 * rather than waiting for a scroll event that may never come.
 */

const bar = () => document.querySelector(".bar");

const renderShell = () =>
  render(
    <MemoryRouter initialEntries={["/"]}>
      <Shell>
        <p>content</p>
      </Shell>
    </MemoryRouter>,
  );

/** Set the offset WITHOUT an event, the way a restored scroll arrives. */
const setOffset = (y: number) => {
  Object.defineProperty(window, "scrollY", {
    value: y,
    writable: true,
    configurable: true,
  });
};

/** Move the window and fire the event a real scroll would. */
const scrollTo = (y: number) =>
  act(() => {
    setOffset(y);
    window.dispatchEvent(new Event("scroll"));
  });

describe("the landing bar", () => {
  it("is transparent at the top of the page", () => {
    setOffset(0);
    renderShell();
    expect(bar()?.classList.contains("bar-quiet")).toBe(true);
  });

  it("takes a surface once the page scrolls under it", () => {
    setOffset(0);
    renderShell();
    scrollTo(300);
    // Without this the nav and the page text render on top of each
    // other, because nothing is behind the bar.
    expect(bar()?.classList.contains("bar-quiet")).toBe(false);
  });

  it("returns to transparent when the reader comes back up", () => {
    setOffset(0);
    renderShell();
    scrollTo(300);
    scrollTo(0);
    expect(bar()?.classList.contains("bar-quiet")).toBe(true);
  });

  it("matches the restored scroll offset on mount", () => {
    // A reload lands mid-page: the browser restores the offset and
    // dispatches no scroll event, so the bar must read the offset
    // itself rather than wait to be told.
    //
    // Under jsdom this passes whether or not the mount sync is present,
    // because the route-change effect also runs on mount here and
    // happens to set the same state. It does NOT cover for it in a real
    // browser -- removing the sync and scrolling Chrome leaves the bar
    // transparent with content running under it. Kept as a regression
    // guard on the OUTCOME, not as proof of the mechanism.
    setOffset(500);
    renderShell();
    expect(bar()?.classList.contains("bar-quiet")).toBe(false);
  });
});
