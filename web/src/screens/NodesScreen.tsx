import { useState } from "react";
import { listNodes } from "../api";
import type { ListNodesQuery, NodeType, NodeStatus } from "../types";
import { NODE_STATUSES, NODE_TYPES } from "../types";
import { useRequest } from "../hooks/useRequest";
import { RequestView } from "../components/ProblemAlert";
import { NodeTable } from "../components/layout";
import { FilterBar, SelectField, TextField } from "../components/formkit";

/**
 * The node registry browser: GET /nodes with the type, region and status
 * filters (they combine with AND). The node id links to the node's page.
 */
export function NodesScreen() {
  const [type, setType] = useState("");
  const [regionId, setRegionId] = useState("");
  const [status, setStatus] = useState("");

  const query: ListNodesQuery = {
    type: type ? (type as NodeType) : undefined,
    regionId: regionId.trim() || undefined,
    status: status ? (status as NodeStatus) : undefined,
  };
  const key = `nodes:${query.type ?? ""}:${query.regionId ?? ""}:${query.status ?? ""}`;
  const state = useRequest(() => listNodes(query), key);

  return (
    <section aria-label="Node registry">
      <h2 style={{ fontSize: "var(--wh-font-size-xl)", margin: 0 }}>Nodes</h2>
      <p style={{ color: "var(--wh-color-text-muted)" }}>
        The authoritative registry of the network's nodes, with each node's current operational profile.
      </p>
      <FilterBar>
        <SelectField label="Node type" value={type} onChange={setType} options={NODE_TYPES} />
        <TextField
          label="Region"
          value={regionId}
          onChange={setRegionId}
          placeholder="any region"
          hint="A regionId, e.g. region-9c21"
        />
        <SelectField label="Status" value={status} onChange={setStatus} options={NODE_STATUSES} />
      </FilterBar>
      <RequestView
        state={state}
        what="nodes"
        isEmpty={(list) => list.nodes.length === 0}
        empty="No node matches these filters."
      >
        {(list) => <NodeTable nodes={list.nodes} linkTo={(id) => encodeURIComponent(id)} />}
      </RequestView>
    </section>
  );
}
