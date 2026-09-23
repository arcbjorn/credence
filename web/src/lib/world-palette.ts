/** The paper view is etched jade glass; the night view keeps its luminous core. */
const LIGHT = {
  shell: ["#f3f7f1", "#d9e6dc", "#adc5b9", "#719487", "#3c6558"],
  rim: "#6a9484", contour: "#396c5d", meridian: "#467d6c",
  grain: ["#466e5c", "#709584"], atmosphere: "64, 126, 104", haze: .10,
  core: ["rgba(255,255,248,.98)", "rgba(250,255,237,.88)", "rgba(240,255,223,.38)", "rgba(211,237,214,.12)"],
  arc: "#276f60", pulse: "#f7fff0", eclipse: "#4a685e",
  moon: ["#e4eee0", "#a0beaa", "#4a7160"], dark: false,
} as const;
const DARK = {
  shell: ["#7b9490", "#475f5a", "#243735", "#111c1c", "#080e0f"],
  rim: "#a5c4b6", contour: "#bacdc0", meridian: "#a6c7b6",
  grain: ["#e6ebd9", "#95b6a9"], atmosphere: "90, 153, 140", haze: .19,
  core: ["rgba(230,244,221,.94)", "rgba(213,235,207,.8)", "rgba(154,194,160,.32)", "rgba(88,150,129,.16)"],
  arc: "#b1d1c1", pulse: "#eff8df", eclipse: "#101817",
  moon: ["#b8c5aa", "#5a7565", "#142722"], dark: true,
} as const;

export const worldPalette = (dark: boolean) => dark ? DARK : LIGHT;
