import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { DataTable, StatusPill } from "@warehouse/ui-kit";
import type { Lane, NodeStatus } from "../types";
import { locationLabel, minutesLabel, typeLabel } from "../lib";

export function PageHeader({ title, subtitle }: { title: string; subtitle: ReactNode }) {
  return (
    <div>
      <h1 style={{ fontSize: "var(--wh-font-size-2xl)", margin: 0 }}>{title}</h1>
      <p style={{ color: "var(--wh-color-text-muted)", marginTop: 4 }}>{subtitle}</p>
    </div>
  );
}

export function Stack({ children, gap = 4 }: { children: ReactNode; gap?: 3 | 4 | 5 }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: `var(--wh-space-${gap})` }}>
      {children}
    </div>
  );
}

/** A label/value list (dl) for a record's scalar fields. */
export function Facts({ items }: { items: { label: string; value: ReactNode }[] }) {
  return (
    <dl
      style={{
        display: "grid",
        gridTemplateColumns: "max-content 1fr",
        gap: "var(--wh-space-2) var(--wh-space-4)",
        margin: 0,
        fontSize: "var(--wh-font-size-sm)",
      }}
    >
      {items.map((item) => (
        <div key={item.label} style={{ display: "contents" }}>
          <dt style={{ color: "var(--wh-color-text-muted)" }}>{item.label}</dt>
          <dd style={{ margin: 0 }}>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

type Tone = "neutral" | "progress" | "success" | "warning" | "danger";

/** A node's lifecycle, carried by StatusPill's tone override. */
const NODE_STATUS_TONE: Record<NodeStatus, Tone> = {
  PROPOSED: "neutral",
  ACTIVE: "progress",
  SUSPENDED: "warning",
  RETIRED: "neutral",
};

export function NodeStatusPill({ status }: { status: NodeStatus }) {
  return <StatusPill status={status} tone={NODE_STATUS_TONE[status]} size="sm" />;
}

/** A lane's status: OPEN lanes move, CLOSED ones are history. */
export function LaneStatusPill({ status }: { status: string }) {
  const tone: Tone = status === "OPEN" ? "progress" : "neutral";
  return <StatusPill status={status} tone={tone} size="sm" />;
}

/** The registry table: one row per node, linking to the node's page. */
export function NodeTable({ nodes, linkTo }: { nodes: NodeLike[]; linkTo: (nodeId: string) => string }) {
  return (
    <DataTable
      rowKey={(n) => n.nodeId}
      rows={nodes}
      columns={[
        { key: "id", header: "Node", render: (n) => <Link to={linkTo(n.nodeId)}>{n.nodeId}</Link> },
        { key: "type", header: "Type", render: (n) => typeLabel(n.nodeType) + (n.subtype ? ` (${n.subtype})` : "") },
        { key: "operator", header: "Operator", render: (n) => n.operator },
        { key: "region", header: "Region", render: (n) => n.regionId },
        { key: "location", header: "Location", render: (n) => locationLabel(n.location) },
        { key: "status", header: "Status", render: (n) => <NodeStatusPill status={n.status} /> },
      ]}
    />
  );
}

/** The lanes table: origin -> destination with mode and transit. */
export function LaneTable({ lanes }: { lanes: Lane[] }) {
  return (
    <DataTable
      rowKey={(l) => l.laneId}
      rows={lanes}
      columns={[
        { key: "id", header: "Lane", render: (l) => l.laneId },
        { key: "origin", header: "Origin", render: (l) => l.originNodeId },
        { key: "destination", header: "Destination", render: (l) => l.destinationNodeId },
        { key: "mode", header: "Mode", render: (l) => l.mode },
        { key: "transit", header: "Transit", align: "right", render: (l) => minutesLabel(l.nominalTransitMinutes) },
        { key: "status", header: "Status", render: (l) => <LaneStatusPill status={l.status} /> },
      ]}
    />
  );
}

/** The minimal node shape NodeTable renders (the wire Node satisfies it). */
export interface NodeLike {
  nodeId: string;
  nodeType: string;
  subtype?: string;
  operator: string;
  regionId: string;
  location: { postalCode: string; countryCode: string; lat?: number; lon?: number };
  status: NodeStatus;
}
