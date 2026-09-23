import { Navigate, Route, Routes, useLocation } from "react-router";
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
import "./styles/weather.css";
import "./styles/controls.css";
import "./styles/navigation.css";
import "./styles/pages.css";
import "./styles/field-guide.css";

/** Rooms own their detailed records; old loan links keep working. */
function LegacyArc() {
  const { search, hash } = useLocation();
  return <Navigate to={{ pathname: "/arc", search, hash }} replace />;
}

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
          <Route path="/lifecycle" element={<LegacyArc />} />
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
