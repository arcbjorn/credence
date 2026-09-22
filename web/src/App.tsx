import { Route, Routes } from "react-router";
import { Shell } from "./components/Shell";
import { AgentOverview } from "./screens/AgentOverview";
import { AssessmentDetail } from "./screens/AssessmentDetail";
import { Comparison_ } from "./screens/Comparison";
import { Enforcement } from "./screens/Enforcement";
import { EvidenceInspector } from "./screens/EvidenceInspector";
import { Lender } from "./screens/Lender";
import { Lifecycle } from "./screens/Lifecycle";
import { IndexedLifecycle } from "./screens/Indexed";
import { PerplPanel } from "./screens/Perpl";
import { Replay } from "./screens/Replay";
import { Start } from "./screens/Start";
import { Observatory } from "./screens/Observatory";
import { EmptyCoordinate, FieldGuide } from "./screens/FieldGuide";
import "./styles/app.css";
import "./styles/premium.css";

/**
 * `docs/09` specifies eight screens. All eight are built here -- the ones that
 * carry the thesis: what an agent can borrow and why (4b), the
 * three-agent comparison (beats 1 and 5), the
 * constraints behind one decision, the loan lifecycle with its forecasts
 * (3, 4 and 7), the lender's epoch ledgers, the prohibited withdrawal (beat 6, which `docs/09` calls
 * the single most convincing moment), and replay (beat 8).
 *
 * The rest are listed in `docs/09` and deliberately NOT stubbed with empty
 * shells: a nav item that opens a blank page reads as broken, where an
 * absent one reads as unbuilt.
 *
 * "Perpl risk" is a tenth: observed market data from the perps venue
 * driving the same three stress scenarios (`docs/08`). It is separate
 * from the assessment screens because it answers a different question --
 * not "what did this account get" but "what does today's market do to
 * it" -- and the comparison only means anything with both columns shown.
 *
 * "Indexed" is a ninth, beyond `docs/09`'s list: the lifecycle rebuilt
 * from the event log by the Envio indexer (`docs/08`). It is separate
 * from "Loan lifecycle" on purpose — that screen renders what the gateway
 * retained, this one what the log alone can prove, and merging them would
 * hide which source each figure came from.
 */
export default function App() {
  return (
    <Shell>
        <Routes>
          <Route path="/" element={<Start />} />
          <Route path="/observatory" element={<Observatory />} />
          <Route path="/atlas" element={<Observatory />} />
          <Route path="/moons" element={<Observatory />} />
          <Route path="/body" element={<Observatory />} />
          <Route path="/log" element={<Observatory />} />
          <Route path="/guide" element={<FieldGuide />} />
          <Route path="/arc" element={<Lifecycle />} />
          <Route path="/agent" element={<AgentOverview />} />
          <Route path="/compare" element={<Comparison_ />} />
          <Route path="/assessment" element={<AssessmentDetail />} />
          <Route path="/lifecycle" element={<Lifecycle />} />
          <Route path="/indexed" element={<IndexedLifecycle />} />
          <Route path="/evidence" element={<EvidenceInspector />} />
          <Route path="/lender" element={<Lender />} />
          <Route path="/enforcement" element={<Enforcement />} />
          <Route path="/perpl" element={<PerplPanel />} />
          <Route path="/replay" element={<Replay />} />
          <Route path="*" element={<EmptyCoordinate />} />
        </Routes>
    </Shell>
  );
}
