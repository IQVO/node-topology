import type { Lane, Node, Region, ServiceArea } from "../types";

/** Wire-shaped fixtures the tests serve through the fetch mock. */

export const fcNode: Node = {
  nodeId: "node-3f6a",
  nodeType: "FULFILLMENT_CENTER",
  subtype: "SORTABLE",
  operator: "VENDOR",
  vendorRef: { vendorId: "vendor-1", vendorNodeCode: "FC-CODE-1" },
  regionId: "region-9c21",
  timezone: "America/Sao_Paulo",
  location: { postalCode: "01310", countryCode: "BR", lat: -23.5614, lon: -46.6559 },
  capabilities: { outbound: true, inbound: false, returns: false, sort: false, lastMile: false, air: false },
  status: "ACTIVE",
  operationalProfile: {
    profileRevision: 1,
    timezone: "America/Sao_Paulo",
    operatingHoursByDay: { monday: { start: "06:00", end: "22:00" } },
    holidays: ["2026-12-25"],
    handlingTimeMinutes: 30,
    cutoffs: [{ localTime: "14:00", days: ["monday"], maxOrders: 10 }],
    throughputCap: { value: 100, timeUnit: "HOUR", unit: "ORDER" },
    declaredBy: "NETWORK_OPS",
    declaredAt: "2026-10-20T09:15:00Z",
  },
  activatedAt: "2026-10-20T09:30:00Z",
};

export const stationNode: Node = {
  nodeId: "node-8b2c",
  nodeType: "DELIVERY_STATION",
  operator: "NETWORK",
  regionId: "region-9c21",
  timezone: "America/Sao_Paulo",
  location: { postalCode: "01550", countryCode: "BR" },
  capabilities: { outbound: false, inbound: false, returns: false, sort: false, lastMile: true, air: false },
  status: "ACTIVE",
  activatedAt: "2026-10-21T10:00:00Z",
};

export const proposedNode: Node = {
  nodeId: "node-1d4e",
  nodeType: "AIR_HUB",
  operator: "NETWORK",
  regionId: "region-9c21",
  timezone: "UTC",
  location: { postalCode: "07310", countryCode: "BR" },
  capabilities: { outbound: false, inbound: false, returns: false, sort: true, lastMile: false, air: true },
  status: "PROPOSED",
};

export const groundLane: Lane = {
  laneId: "lane-77aa",
  originNodeId: "node-4c10",
  destinationNodeId: "node-8b2c",
  mode: "GROUND",
  nominalTransitMinutes: 240,
  status: "OPEN",
};

export const northRegion: Region = { regionId: "region-9c21", code: "NORTH", name: "North" };
export const southRegion: Region = { regionId: "region-4a77", code: "SOUTH", name: "South" };

export const prefixArea: ServiceArea = {
  postalPrefix: "013",
  deliveryStationNodeId: "node-8b2c",
  regionId: "region-9c21",
  assignmentVersion: 1,
};
