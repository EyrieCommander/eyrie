import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./index.css";
import App from "./App";

// StrictMode removed intentionally: it double-mounts effects in dev mode,
// which breaks WebSocket + PTY resources in the Terminal component (the
// second mount races the first's cleanup, producing dead connections).
createRoot(document.getElementById("root")!).render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
);
