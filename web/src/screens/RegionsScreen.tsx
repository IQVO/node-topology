import { listRegions } from "../api";
import { useRequest } from "../hooks/useRequest";
import { RequestView } from "../components/ProblemAlert";
import { DataTable } from "@warehouse/ui-kit";

/**
 * The region browser: GET /regions. Regions are few and not paginated; nodes
 * and lanes are filtered by a region's id.
 */
export function RegionsScreen() {
  const state = useRequest(() => listRegions(), "regions");

  return (
    <section aria-label="Region registry">
      <h2 style={{ fontSize: "var(--wh-font-size-xl)", margin: 0 }}>Regions</h2>
      <p style={{ color: "var(--wh-color-text-muted)" }}>
        The partitions of the network; filter nodes and lanes by a region's id.
      </p>
      <RequestView state={state} what="regions" isEmpty={(rows) => rows.length === 0} empty="No region is defined.">
        {(regions) => (
          <DataTable
            rowKey={(r) => r.regionId}
            rows={regions}
            columns={[
              { key: "id", header: "Region", render: (r) => r.regionId },
              { key: "code", header: "Code", render: (r) => r.code },
              { key: "name", header: "Name", render: (r) => r.name },
            ]}
          />
        )}
      </RequestView>
    </section>
  );
}
