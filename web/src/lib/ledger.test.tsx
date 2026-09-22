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

  it("gives each claim its support and evidence", () => {
    renderStart();
    for (const claim of CLAIMS) {
      expect(screen.getByText(claim.support)).toBeInTheDocument();
      expect(screen.getByText(claim.detail)).toBeInTheDocument();
    }
  });
});
