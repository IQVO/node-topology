import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";
import App from "./App";
import { json, mockApi } from "./test/fetchMock";
import { fcNode, groundLane, northRegion } from "./test/fixtures";

/**
 * Mounts App exactly the way the console shell does: inside a splat route
 * (`/node-topology/*`), which is the case its relative sub-nav links must
 * survive (see TopologyLayout).
 */
function mountHostedAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/node-topology/*" element={<App />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("App (as node_topology_mfe/App under the shell's splat route)", () => {
  it("lands on the node registry and navigates the sections with relative links", async () => {
    mockApi({
      "GET /nodes": json({ nodes: [fcNode] }),
      "GET /lanes": json({ lanes: [groundLane] }),
      "GET /regions": json([northRegion]),
    });
    mountHostedAt("/node-topology");
    expect(await screen.findByText("node-3f6a")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("link", { name: "Lanes" }));
    expect(await screen.findByText("lane-77aa")).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "Regions" }));
    expect(await screen.findByText("NORTH")).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "Nodes" }));
    expect(await screen.findByText("node-3f6a")).toBeInTheDocument();
  });

  it("follows a node row into the node's own page", async () => {
    mockApi({
      "GET /nodes": json({ nodes: [fcNode] }),
      "GET /nodes/node-3f6a": json(fcNode),
    });
    mountHostedAt("/node-topology");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("link", { name: "node-3f6a" }));
    expect(await screen.findByText("Operational profile, revision 1")).toBeInTheDocument();
  });
});
