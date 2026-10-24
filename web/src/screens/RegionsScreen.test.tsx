import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { RegionsScreen } from "./RegionsScreen";
import { json, mockApi } from "../test/fetchMock";
import { northRegion, southRegion } from "../test/fixtures";

function mount() {
  return render(
    <MemoryRouter initialEntries={["/regions"]}>
      <RegionsScreen />
    </MemoryRouter>,
  );
}

describe("RegionsScreen", () => {
  it("lists every region with its code and name", async () => {
    mockApi({ "GET /regions": json([northRegion, southRegion]) });
    mount();
    expect(await screen.findByText("region-9c21")).toBeInTheDocument();
    expect(screen.getByText("NORTH")).toBeInTheDocument();
    expect(screen.getByText("North")).toBeInTheDocument();
    expect(screen.getByText("region-4a77")).toBeInTheDocument();
    expect(screen.getByText("South")).toBeInTheDocument();
  });

  it("shows the empty note when no region is defined", async () => {
    mockApi({ "GET /regions": json([]) });
    mount();
    expect(await screen.findByText("No region is defined.")).toBeInTheDocument();
  });
});
