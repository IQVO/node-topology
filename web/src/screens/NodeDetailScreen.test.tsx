import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { NodeDetailRoute } from "./NodeDetailScreen";
import { json, mockApi, problem } from "../test/fetchMock";
import { fcNode, stationNode } from "../test/fixtures";

function mountAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/nodes/:nodeId" element={<NodeDetailRoute />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("NodeDetailRoute", () => {
  it("shows the node's identity, capabilities and current profile revision", async () => {
    mockApi({ "GET /nodes/node-3f6a": json(fcNode) });
    mountAt("/nodes/node-3f6a");
    expect(await screen.findByText("node-3f6a")).toBeInTheDocument();
    expect(screen.getByText(/operated by VENDOR · region region-9c21/)).toBeInTheDocument();
    expect(screen.getByText("01310, BR (-23.5614, -46.6559)")).toBeInTheDocument();
    expect(screen.getByText("outbound")).toBeInTheDocument();
    expect(screen.getByText("vendor-1 / FC-CODE-1")).toBeInTheDocument();
    expect(screen.getByText("Operational profile, revision 1")).toBeInTheDocument();
    expect(screen.getByText("monday 06:00–22:00")).toBeInTheDocument();
    expect(screen.getByText("100 ORDER per HOUR")).toBeInTheDocument();
    expect(screen.getByText("2026-10-20 09:15:00Z")).toBeInTheDocument();
  });

  it("says so when no profile was ever declared", async () => {
    mockApi({ "GET /nodes/node-8b2c": json(stationNode) });
    mountAt("/nodes/node-8b2c");
    expect(await screen.findByText("No operational profile has been declared for this node yet.")).toBeInTheDocument();
  });

  it("renders the problem body of an unknown node", async () => {
    mockApi({
      "GET /nodes/node-nope": problem(404, "not-found", "Resource not found", "no node with id node-nope"),
    });
    mountAt("/nodes/node-nope");
    expect(await screen.findByRole("alert")).toHaveTextContent("Resource not found");
    expect(screen.getByText(/no node with id node-nope/)).toBeInTheDocument();
  });
});
