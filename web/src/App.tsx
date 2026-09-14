import { NavLink, Route, Routes, Navigate } from "react-router";
import { AgentOverview } from "./screens/AgentOverview";
import { AssessmentDetail } from "./screens/AssessmentDetail";
import { Enforcement } from "./screens/Enforcement";
import { Replay } from "./screens/Replay";
import "./styles/app.css";

/**
 * `docs/09` specifies eight screens. Four are built here -- the ones that
 * carry the thesis: what an agent can borrow and why (4b), the
 * constraints behind one decision, the prohibited withdrawal (beat 6,
 * which `docs/09` calls the single most convincing moment), and replay
 * (beat 8).
 *
 * The rest are listed in `docs/09` and deliberately NOT stubbed with empty
 * shells: a nav item that opens a blank page reads as broken, where an
 * absent one reads as unbuilt.
 */
export default function App() {
  return (
    <div className="app">
      <nav className="nav">
        <h1>Credence</h1>
        <p className="tagline">Autonomous credit markets for AI agents</p>
        <NavLink to="/agent">Agent overview</NavLink>
        <NavLink to="/assessment">Assessment detail</NavLink>
        <NavLink to="/enforcement">Enforcement</NavLink>
        <NavLink to="/replay">Replay</NavLink>
      </nav>
      <main className="main">
        <Routes>
          <Route path="/" element={<Navigate to="/agent" replace />} />
          <Route path="/agent" element={<AgentOverview />} />
          <Route path="/assessment" element={<AssessmentDetail />} />
          <Route path="/enforcement" element={<Enforcement />} />
          <Route path="/replay" element={<Replay />} />
        </Routes>
      </main>
    </div>
  );
}
