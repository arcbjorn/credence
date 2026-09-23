/** The paper view is etched jade glass; the night view keeps its luminous core. */
const LIGHT = {
  shell: ["#f3f7f1", "#d9e6dc", "#adc5b9", "#719487", "#3c6558"],
  rim: "#6a9484", contour: "#396c5d", meridian: "#467d6c",
  grain: ["#466e5c", "#709584"], atmosphere: "64, 126, 104", haze: .10,
  core: ["rgba(255,255,248,.98)", "rgba(250,255,237,.88)", "rgba(240,255,223,.38)", "rgba(211,237,214,.12)"],
  arc: "#276f60", pulse: "#f7fff0",
  eclipse: ["#a8b9a9", "#789184", "#466356", "#2d473c", "#20392f"],
  eclipseRim: ["#c8d4c6", "#678173", "#354d40"],
  moon: ["#e4eee0", "#a0beaa", "#4a7160"], dark: false,
} as const;
const DARK = {
  shell: ["#7b9490", "#475f5a", "#243735", "#111c1c", "#080e0f"],
  rim: "#a5c4b6", contour: "#bacdc0", meridian: "#a6c7b6",
  grain: ["#e6ebd9", "#95b6a9"], atmosphere: "90, 153, 140", haze: .19,
  core: ["rgba(230,244,221,.94)", "rgba(213,235,207,.8)", "rgba(154,194,160,.32)", "rgba(88,150,129,.16)"],
  arc: "#b1d1c1", pulse: "#eff8df",
  eclipse: ["#607c6c", "#344d40", "#1a3026", "#0c1b14", "#070f0b"],
  eclipseRim: ["#94ab94", "#354e3d", "#15261c"],
  moon: ["#b8c5aa", "#5a7565", "#142722"], dark: true,
} as const;

export const worldPalette = (dark: boolean) => dark ? DARK : LIGHT;

/**
 * One pastel per agent, for the arcs and the dots at either end.
 *
 * Every loan used to be drawn in the single `arc` jade, so five loans to
 * one borrower stacked into what looked like a single line. Colour is now
 * what separates one agent's traffic from another's.
 *
 * **Two sets, not one set dimmed.** A pastel that reads correctly on warm
 * paper is washed out to near-invisible on the dark jade ground, and a
 * tint that glows there is too pale to see on paper. So the light set is
 * mid-toned enough to hold a hairline against `#f9f5ef`, and the dark set
 * is the same hue lifted in lightness and dropped in saturation to sit on
 * `#080e0f` without vibrating.
 *
 * Hues are spaced around the wheel and deliberately avoid the shell's
 * jade: an arc that shares the globe's own colour disappears into it.
 * They also stay clear of the vermilion ALARM register, which `docs/09`
 * reserves for refusals and realized loss -- an agent tinted the colour
 * of a denial would read as one.
 */
const AGENT_LIGHT = [
  "#c2763f", // amber
  "#5a7fb8", // periwinkle
  "#9a6bab", // lilac
  "#3f8f96", // teal-cyan
  "#a8763d", // ochre
  "#7b8a3e", // olive
  "#b3617d", // rose
  "#5d74a8", // slate blue
] as const;

const AGENT_DARK = [
  "#f0b988", // amber
  "#a9c3ec", // periwinkle
  "#d3aee0", // lilac
  "#8ad4d8", // teal-cyan
  "#e6c38f", // ochre
  "#c4d18c", // olive
  "#eeaabe", // rose
  "#aab9e4", // slate blue
] as const;

/**
 * The colour for agent `i`, wrapping if there are more agents than hues.
 *
 * Wrapping repeats a hue rather than generating one, because a generated
 * colour cannot be checked for contrast ahead of time. Eight distinct
 * agents is well past what the globe shows today.
 */
export const agentColor = (index: number, dark: boolean): string => {
  const set = dark ? AGENT_DARK : AGENT_LIGHT;
  return set[((index % set.length) + set.length) % set.length]!;
};

/** How many agents get a hue of their own before the set repeats. */
export const AGENT_COLORS = AGENT_LIGHT.length;
