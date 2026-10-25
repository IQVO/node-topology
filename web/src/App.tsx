import { Navigate, NavLink, Outlet, Route, Routes } from "react-router-dom";
import { LanesScreen } from "./screens/LanesScreen";
import { NodeDetailRoute } from "./screens/NodeDetailScreen";
import { NodesScreen } from "./screens/NodesScreen";
import { RegionsScreen } from "./screens/RegionsScreen";

const SUB_NAV = [
  { to: "nodes", label: "Nodes" },
  { to: "lanes", label: "Lanes" },
  { to: "regions", label: "Regions" },
];

const linkStyle = ({ isActive }: { isActive: boolean }) => ({
  display: "inline-flex",
  padding: "6px 12px",
  borderRadius: "var(--wh-radius-pill)",
  fontSize: "var(--wh-font-size-sm)",
  fontWeight: isActive ? 600 : 500,
  color: isActive ? "var(--wh-color-text)" : "var(--wh-color-text-muted)",
  background: isActive ? "var(--wh-color-accent-muted)" : "transparent",
  textDecoration: "none",
});

/** The sub-nav, rendered by a layout route with the path "/".
 *
 *  The console mounts this component inside its own `<Route
 *  path="/node-topology/*">`. A relative `<NavLink>` rendered straight in
 *  that splat route resolves against the FULL current URL (react-router 7
 *  uses the last path-contributing match's pathname, splat included), so from
 *  /node-topology/lanes the link `nodes` would point at
 *  /node-topology/lanes/nodes. A pathless layout route does not help
 *  (react-router drops pathless routes from relative resolution); a layout
 *  route WITH path "/" does: its own match is the mount point, under any
 *  prefix and standalone at `/`. (Same fix as the other fleet remotes;
 *  App.test.tsx mounts under the host's splat route to prove it.) */
function TopologyLayout() {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-5)" }}>
      <nav aria-label="Node topology sections" style={{ display: "flex", gap: "var(--wh-space-2)" }}>
        {SUB_NAV.map((item) => (
          <NavLink key={item.to} to={item.to} style={linkStyle}>
            {item.label}
          </NavLink>
        ))}
      </nav>
      <Outlet />
    </div>
  );
}

/** Exposed as node_topology_mfe/App via Module Federation. Takes NO props:
 *  the console mounts it under `/node-topology/*` and provides the
 *  BrowserRouter, the design tokens and `window.__WAREHOUSE_CONFIG__`. Routes
 *  here are RELATIVE, so the component works identically under that prefix
 *  (in the shell) or at / (standalone dev, see main.tsx). It calls ONLY this
 *  context's own API (/api/node-topology) and is read-only end to end.
 *
 *   - (index): redirects to nodes.
 *   - nodes: the registry with type/region/status filters; nodes/:nodeId:
 *     one node with its current operational profile.
 *   - lanes: the lane registry with origin/destination filters.
 *   - regions: the region registry. */
export default function App() {
  return (
    <Routes>
      <Route path="/" element={<TopologyLayout />}>
        <Route index element={<Navigate to="nodes" replace />} />
        <Route path="nodes" element={<NodesScreen />} />
        <Route path="nodes/:nodeId" element={<NodeDetailRoute />} />
        <Route path="lanes" element={<LanesScreen />} />
        <Route path="regions" element={<RegionsScreen />} />
      </Route>
    </Routes>
  );
}
