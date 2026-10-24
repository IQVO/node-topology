import { useParams } from "react-router-dom";
import { getNode } from "../api";
import { useRequest } from "../hooks/useRequest";
import { RequestView } from "../components/ProblemAlert";
import { Facts, NodeStatusPill, PageHeader, Stack } from "../components/layout";
import { capabilityWords, locationLabel, typeLabel, utcLabel } from "../lib";
import type { Node } from "../types";

/** GET /nodes/{nodeId}: one node and its current operational profile. */
export function NodeDetailRoute() {
  const { nodeId } = useParams<{ nodeId: string }>();
  const state = useRequest(nodeId ? () => getNode(nodeId) : null, `node:${nodeId}`);

  return (
    <RequestView state={state} what={`node ${nodeId}`}>
      {(node) => <NodeDetail node={node} />}
    </RequestView>
  );
}

function NodeDetail({ node }: { node: Node }) {
  const profile = node.operationalProfile;
  return (
    <Stack gap={5}>
      <PageHeader
        title={node.nodeId}
        subtitle={
          <>
            {typeLabel(node.nodeType)}
            {node.subtype ? ` (${node.subtype})` : ""} · operated by {node.operator} · region {node.regionId}
          </>
        }
      />
      <Facts
        items={[
          { label: "Status", value: <NodeStatusPill status={node.status} /> },
          { label: "Location", value: locationLabel(node.location) },
          { label: "Timezone", value: node.timezone },
          { label: "Capabilities", value: capabilityWords(node.capabilities).join(", ") || "none" },
          {
            label: "Vendor's own code",
            value: node.vendorRef ? `${node.vendorRef.vendorId} / ${node.vendorRef.vendorNodeCode}` : "—",
          },
          { label: "Activated at", value: utcLabel(node.activatedAt) },
          ...(node.suspendReason ? [{ label: "Suspend reason", value: node.suspendReason }] : []),
        ]}
      />
      {profile ? <ProfileFacts nodeId={node.nodeId} profile={profile} /> : <ProfileAbsent />}
    </Stack>
  );
}

function ProfileFacts({
  nodeId,
  profile,
}: {
  nodeId: string;
  profile: NonNullable<Node["operationalProfile"]>;
}) {
  const hours = Object.entries(profile.operatingHoursByDay)
    .map(([day, h]) => `${day} ${h.start}–${h.end}`)
    .join(", ");
  return (
    <section aria-label={`Operational profile of ${nodeId}`}>
      <h3 style={{ fontSize: "var(--wh-font-size-lg)", margin: 0 }}>
        Operational profile, revision {profile.profileRevision}
      </h3>
      <Facts
        items={[
          { label: "Declared by", value: profile.declaredBy },
          { label: "Declared at", value: utcLabel(profile.declaredAt) },
          { label: "Timezone", value: profile.timezone },
          { label: "Operating hours", value: hours || "—" },
          { label: "Holidays", value: profile.holidays.join(", ") || "—" },
          { label: "Handling time", value: `${profile.handlingTimeMinutes} min` },
          {
            label: "Cutoffs",
            value:
              profile.cutoffs
                .map((c) => `${c.localTime}${c.days?.length ? ` (${c.days.join(" ")})` : ""}`)
                .join(", ") || "—",
          },
          {
            label: "Throughput cap",
            value: `${profile.throughputCap.value} ${profile.throughputCap.unit} per ${profile.throughputCap.timeUnit}`,
          },
          ...(profile.sortDwellMinutes !== undefined
            ? [{ label: "Sort dwell", value: `${profile.sortDwellMinutes} min` }]
            : []),
        ]}
      />
    </section>
  );
}

function ProfileAbsent() {
  return (
    <p style={{ color: "var(--wh-color-text-muted)" }}>
      No operational profile has been declared for this node yet.
    </p>
  );
}
