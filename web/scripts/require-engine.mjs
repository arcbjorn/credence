// Fail with a sentence, not a module resolution error.
//
// `src/engine/` is a build artifact: `scripts/build-wasm.sh` compiles it
// from `risk/crates/risk-wasm`, and it is gitignored so a second copy
// cannot fall behind the first. That is the right call, and it means a
// fresh clone has no engine until something builds it.
//
// `src/lib/engine.ts` imports it STATICALLY, so without it `pnpm test`
// and `pnpm typecheck` fail with three `TS2307: Cannot find module
// '../engine/risk_wasm.js'` lines and no hint about what to run. The
// documented path (`scripts/web.sh`) builds it first and works fine;
// this is for the reader who runs `pnpm test` directly, which the
// comment in `src/lib/fixtures.ts` promises will work in a fresh clone.
//
// Found 20 September by cloning the repository to a temp directory and
// following it as a reviewer would.
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const engine = resolve(here, "..", "src", "engine", "risk_wasm.js");

if (!existsSync(engine)) {
  process.stderr.write(
    "\n" +
      "The WebAssembly engine is not built.\n\n" +
      "  src/engine/ is generated from risk/crates/risk-wasm and is\n" +
      "  gitignored, so a fresh clone does not have it. Two tests and\n" +
      "  the typecheck import it directly.\n\n" +
      "Build it, then re-run:\n\n" +
      "  bash ../scripts/build-wasm.sh\n\n" +
      "Or take the documented path, which does this and more:\n\n" +
      "  bash ../scripts/web.sh --no-serve\n\n",
  );
  process.exit(1);
}
