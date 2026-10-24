import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { NodesScreen } from "./NodesScreen";
import { json, mockApi, pending } from "../test/fetchMock";
import { fcNode, proposedNode, stationNode } from "../test/fixtures";

function mount() {
  return render(
    <MemoryRouter initialEntries={["/nodes"]}>
      <NodesScreen />
    </MemoryRouter>,
  );
}

describe("NodesScreen", () => {
  it("lists the registry with each node's human type and status", async () => {
    mockApi({ "GET /nodes": json({ nodes: [fcNode, stationNode, proposedNode] }) });
    mount();
    expect(await screen.findByText("node-3f6a")).toBeInTheDocument();
    expect(screen.getByText("fulfillment centre (SORTABLE)")).toBeInTheDocument();
    expect(screen.getByText("delivery station")).toBeInTheDocument();
    expect(screen.getByText("air hub")).toBeInTheDocument();
    expect(screen.getByText("01310, BR (-23.5614, -46.6559)")).toBeInTheDocument();
    // Scope to the table: the status filter's <option> carries the same words.
    const table = screen.getByRole("table");
    expect(within(table).getAllByText("ACTIVE")).toHaveLength(2);
    expect(within(table).getByText("PROPOSED")).toBeInTheDocument();
  });

  it("sends the type, region and status filters to GET /nodes", async () => {
    const api = mockApi({ "GET /nodes": json({ nodes: [] }) });
    mount();
    const user = userEvent.setup();
    await user.selectOptions(screen.getByLabelText("Node type"), "DELIVERY_STATION");
    await user.type(screen.getByLabelText(/^Region/), "region-9c21");
    await user.selectOptions(screen.getByLabelText("Status"), "ACTIVE");
    await waitFor(() => {
      const last = api.to("GET /nodes").at(-1);
      expect(last?.query.get("type")).toBe("DELIVERY_STATION");
      expect(last?.query.get("region")).toBe("region-9c21");
      expect(last?.query.get("status")).toBe("ACTIVE");
    });
  });

  it("shows the empty note when nothing matches", async () => {
    mockApi({ "GET /nodes": json({ nodes: [] }) });
    mount();
    expect(await screen.findByText("No node matches these filters.")).toBeInTheDocument();
  });

  it("renders the problem body of a failed load", async () => {
    mockApi({
      "GET /nodes": pending(),
    });
    const { rerender } = mount();
    rerender(
      <MemoryRouter initialEntries={["/nodes"]}>
        <NodesScreen />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Loading nodes…");
  });
});
