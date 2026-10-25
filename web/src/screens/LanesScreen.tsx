import { useState } from "react";
import { listLanes } from "../api";
import type { ListLanesQuery } from "../types";
import { useRequest } from "../hooks/useRequest";
import { RequestView } from "../components/ProblemAlert";
import { LaneTable } from "../components/layout";
import { FilterBar, TextField } from "../components/formkit";

/**
 * The lane browser: GET /lanes with the origin and destination filters (they
 * combine with AND). Open and closed lanes are listed together, told apart
 * by status.
 */
export function LanesScreen() {
  const [origin, setOrigin] = useState("");
  const [destination, setDestination] = useState("");

  const query: ListLanesQuery = {
    origin: origin.trim() || undefined,
    destination: destination.trim() || undefined,
  };
  const key = `lanes:${query.origin ?? ""}:${query.destination ?? ""}`;
  const state = useRequest(() => listLanes(query), key);

  return (
    <section aria-label="Lane registry">
      <h2 style={{ fontSize: "var(--wh-font-size-xl)", margin: 0 }}>Lanes</h2>
      <p style={{ color: "var(--wh-color-text-muted)" }}>
        The directed GROUND / AIR connections between nodes; open and closed lanes together.
      </p>
      <FilterBar>
        <TextField label="Origin node" value={origin} onChange={setOrigin} placeholder="any origin" />
        <TextField label="Destination node" value={destination} onChange={setDestination} placeholder="any destination" />
      </FilterBar>
      <RequestView
        state={state}
        what="lanes"
        isEmpty={(list) => list.lanes.length === 0}
        empty="No lane matches these filters."
      >
        {(list) => <LaneTable lanes={list.lanes} />}
      </RequestView>
    </section>
  );
}
