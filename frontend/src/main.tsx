import { createRoot } from "react-dom/client";
import { AppRouter } from "./app/routes";

const container = document.getElementById("root");
if (!container) {
  throw new Error("#root element not found");
}

createRoot(container).render(<AppRouter />);
