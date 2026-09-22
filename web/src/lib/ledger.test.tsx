import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { Start } from "../screens/Start";
import { CLAIMS, EARNED, UNEARNED } from "./claims";

/**
 * The landing page's ledger, rendered.
 *
 * Every other test in this directory checks the DATA a screen depends
 * on, which is why they all survived the landing page being rewritten
 * from an essay into a split layout -- correct, but it means nothing
 * asserted the claims ledger reaches the page at all.
 *
 * The property that matters is the honest one: the unearned claims must
 * be present, in the same list, at the same level as the rest. A page
 * that quietly dropped them would still pass every data test and would
 * be exactly the marketing artifact this project is trying not to be.
 */

const renderStart = () =>
  render(
    <MemoryRouter>
      <Start />
    </MemoryRouter>,
  );

describe("the claims ledger on the landing page", () => {
  it("renders every claim", () => {
    renderStart();
    for (const claim of CLAIMS) {
      expect(
        screen.getByText(claim.title),
        `${claim.title} is missing`,
      ).toBeInTheDocument();
    }
  });

  it("shows the in-progress capabilities rather than hiding them", () => {
    renderStart();
    const notYet = screen.getAllByText("building");
    expect(notYet.length).toBe(UNEARNED);
    // Both sit in the same list as the earned ones: same element type,
    // same parent. A footnote would be a different container.
    const ledger = document.querySelector(".ledger");
    for (const el of notYet) {
      expect(ledger?.contains(el)).toBe(true);
    }
  });

  it("states the working count the ledger actually contains", () => {
    renderStart();
    expect(screen.getAllByText("live").length).toBe(EARNED);
    // The summary line must agree with the rows beneath it. Matched on
    // the COUNTS rather than on a fixed sentence, so rewording the copy
    // does not fail this and changing a number does.
    expect(
      screen.getByText(`${EARNED} of ${EARNED + UNEARNED} work today.`),
    ).toBeInTheDocument();
  });

  it("puts the object, the claim and the way in together", () => {
    // These five sat below the fold for several revisions: a visitor saw
    // a headline, a sphere and a list, and the primary call to action was
    // off screen entirely. jsdom has no layout, so this pins ORDER and
    // presence rather than pixels -- the object must come with the claim
    // and the way in, not a screen above them.
    renderStart();
    const left = document.querySelector(".split-left")!;
    const order = [".hero", ".hero-objects", ".hero-lede", ".start-enter"].map(
      (sel) => {
        const el = left.querySelector(sel);
        expect(el, `${sel} is missing from the opening`).not.toBeNull();
        return [...left.querySelectorAll("*")].indexOf(el!);
      },
    );
    // Strictly increasing: each follows the one before it.
    for (let i = 1; i < order.length; i++) {
      expect(order[i]!).toBeGreaterThan(order[i - 1]!);
    }
  });

  it("keeps a slot for the book beside the object", () => {
    // The eclipse states utilization at a glance and sat at 1184px,
    // below the fold. It now shares a row with the globe.
    //
    // Its CONTENTS cannot be asserted here: the component loads its
    // figures over fetch, and under jsdom the bundle is absent, so both
    // the globe and the eclipse correctly render nothing rather than
    // inventing a book. This pins the container that puts them side by
    // side; `shapes.test.tsx` covers the eclipse's own geometry.
    renderStart();
    expect(document.querySelector(".hero-objects")).not.toBeNull();
  });

  it("gives each claim its support and evidence", () => {
    renderStart();
    for (const claim of CLAIMS) {
      expect(screen.getByText(claim.support)).toBeInTheDocument();
      expect(screen.getByText(claim.detail)).toBeInTheDocument();
    }
  });
});
