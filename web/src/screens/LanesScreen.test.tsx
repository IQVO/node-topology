import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { LanesScreen } from "./LanesScreen";
import { json, mockApi } from "../test/fetchMock";
import { groundLane } from "../test/fixtures";

function mount() {
  return render(
    <MemoryRouter initialEntries={["/lanes"]}>
      <LanesScreen />
    </MemoryRouter>,
  );
}

describe("LanesScreen", () => {
  it("lists lanes with mode, transit and status", async () => {
    mockApi({ "GET /lanes": json({ lanes: [groundLane] }) });
    mount();
    expect(await screen.findByText("lane-77aa")).toBeInTheDocument();
    expect(screen.getByText("node-4c10")).toBeInTheDocument();
    expect(screen.getByText("node-8b2c")).toBeInTheDocument();
    expect(screen.getByText("GROUND")).toBeInTheDocument();
    expect(screen.getByText("4 h")).toBeInTheDocument();
    expect(screen.getByText("OPEN")).toBeInTheDocument();
  });

  it("sends the origin and destination filters", async () => {
    const api = mockApi({ "GET /lanes": json({ lanes: [] }) });
    mount();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Origin node"), "node-4c10");
    await user.type(screen.getByLabelText("Destination node"), "node-8b2c");
    await waitFor(() => {
      const last = api.to("GET /lanes").at(-1);
      expect(last?.query.get("origin")).toBe("node-4c10");
      expect(last?.query.get("destination")).toBe("node-8b2c");
    });
  });

  it("shows the empty note when nothing matches", async () => {
    mockApi({ "GET /lanes": json({ lanes: [] }) });
    mount();
    expect(await screen.findByText("No lane matches these filters.")).toBeInTheDocument();
  });
});
