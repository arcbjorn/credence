# The physics

One object with laws. Every visual property means a figure, and every
figure comes from a retained fixture or a transaction-derived record
(`docs/11`). If something on screen cannot name its source, it is not in
the physics and it does not ship.

The test for any new element: **what is this in the physics?** If the
answer is "nothing", cut it. The second test: **which loaded figure
drives it?** If the answer is "none", cut it — a shader driven by an
invented number is a claim the bundle never made, and this project spends
its credibility on not doing that.

## Laws in force

| Property | Physical form | Source |
|---|---|---|
| Capacity | Radius of the body, by AREA | `capacity.agent_capacity` |
| Score | Shell temperature, moss → teal | `capacity.score` |
| APR | Weight of the body's edge | `terms.apr_bps` |
| Binding constraint | The limit ring: dotted when history bounds it, solid when stress does | `capacity.agent_binding` |
| Each loan | A node on the shell, with an arc to the next | one per origination |
| Refusal | No body at all, vermilion limit ring | `agent_capacity == 0` |
| **Core** | Brightness at the centre: the lender's idle cash | `vault idle cash` check |
| **Atmosphere** | Rim thickness, as `1 - utilization`. Thick = padded, thin = vacuum | `principal outstanding / (idle + outstanding)` |
| **Scar** | A dark crater on the shell that does not heal | `vault realized loss` |

## Laws deliberately NOT in force

These are good ideas with no data behind them in this release. Building
them would mean inventing figures.

- **Moons** — isolated markets. There is one facility, so a sky of moons
  would be a catalogue of things that do not exist.
- **Storms** — localized utilization per market. Needs several books;
  there is one.
- **Constellations, guilds** — needs a population of agents. There are
  three profiles.
- **Day/night terminator** — needs live activity across time zones. The
  bundle is a static capture with a recorded timestamp.
- **Dead agents as dark matter** — no deregistration events are retained.
- **Draw-to-lend** — origination is a signed transaction, not a gesture;
  a drag that mimes one would be a toy.

When the data arrives, the law gets added here first, then built.

## Colour

The three registers of `docs/09` stay distinct and are never collapsed
into one accent:

- MACHINE (teal) — the stress model, identity, hashes
- GROWTH (moss) — capacity, history, what compounds
- ALARM (vermilion) — refusals, denials, realized loss

A single-accent palette would render a denial and a growing limit
identically, which is the one thing this interface exists to prevent.

## Motion

Gravity, not UI kits. Constant rotation, no easing on the spin. No
bounce, no spring. Atmosphere changes lerp rather than snap. Everything
respects `prefers-reduced-motion`.

## Copy

Short. Physical. If you removed the product name, the sentence should
still sound like a place.

Good: *The core is bright. Two cells under watch. Every agent is a
coordinate.*

Bad: *next-gen, seamless, empower, supercharge, ecosystem.*
