import { beforeEach, describe, expect, it } from "vitest";
import { getRegion, getNode, listLanes, listNodes, listRegions, resolveServiceArea, toApiError } from "./api";
import { json, mockApi, problem } from "./test/fetchMock";
import { fcNode, groundLane, northRegion, prefixArea } from "./test/fixtures";

beforeEach(() => {
  mockApi({});
});

describe("listNodes", () => {
  it("sends the type, region and status filters and normalizes the list", async () => {
    const api = mockApi({});
    api.set("GET /nodes", json({ nodes: [fcNode] }));
    const res = await listNodes({ type: "FULFILLMENT_CENTER", regionId: "region-9c21", status: "ACTIVE" });
    expect(res.nodes).toHaveLength(1);
    expect(api.to("GET /nodes")[0].query.get("type")).toBe("FULFILLMENT_CENTER");
    expect(api.to("GET /nodes")[0].query.get("region")).toBe("region-9c21");
    expect(api.to("GET /nodes")[0].query.get("status")).toBe("ACTIVE");
  });

  it("omits unset filters from the query string", async () => {
    const api = mockApi({});
    api.set("GET /nodes", json({ nodes: [] }));
    await listNodes({});
    expect(api.to("GET /nodes")[0].query.toString()).toBe("");
  });

  it("a missing list is an empty list, never undefined", async () => {
    mockApi({ "GET /nodes": json({}) });
    const res = await listNodes({});
    expect(res.nodes).toEqual([]);
  });
});

describe("getNode", () => {
  it("reads one node by id", async () => {
    const api = mockApi({ "GET /nodes/node-3f6a": json(fcNode) });
    const node = await getNode("node-3f6a");
    expect(node.nodeId).toBe("node-3f6a");
    expect(api.to("GET /nodes/node-3f6a")).toHaveLength(1);
  });

  it("an unknown id surfaces the problem body", async () => {
    mockApi({
      "GET /nodes/node-nope": problem(404, "not-found", "Resource not found", "no node with id node-nope"),
    });
    const err = await getNode("node-nope").catch((e: unknown) => e);
    expect(toApiError(err)).toMatchObject({ status: 404, slug: "not-found", title: "Resource not found" });
  });
});

describe("listLanes", () => {
  it("filters by origin and destination", async () => {
    const api = mockApi({ "GET /lanes": json({ lanes: [groundLane] }) });
    const res = await listLanes({ origin: "node-4c10" });
    expect(res.lanes[0].laneId).toBe("lane-77aa");
    expect(api.to("GET /lanes")[0].query.get("origin")).toBe("node-4c10");
    expect(api.to("GET /lanes")[0].query.get("destination")).toBeNull();
  });
});

describe("resolveServiceArea", () => {
  it("resolves a postal prefix to its delivery station", async () => {
    mockApi({ "GET /service-areas/013": json(prefixArea) });
    const area = await resolveServiceArea("013");
    expect(area.deliveryStationNodeId).toBe("node-8b2c");
    expect(area.assignmentVersion).toBe(1);
  });
});

describe("getRegion / listRegions", () => {
  it("reads one region and lists them all", async () => {
    const api = mockApi({ "GET /regions": json([northRegion]), "GET /regions/region-9c21": json(northRegion) });
    expect(await getRegion("region-9c21")).toEqual(northRegion);
    expect(await listRegions()).toEqual([northRegion]);
    expect(api.to("GET /regions")).toHaveLength(1);
  });
});

describe("toApiError", () => {
  it("wraps a network failure with status 0", async () => {
    mockApi({ "GET /nodes/node-3f6a": problem(503, "dependency-unavailable", "No", "dead") });
    const err = toApiError(new TypeError("fetch failed"));
    expect(err.status).toBe(0);
    expect(err.title).toBe("Network error");
  });
});
