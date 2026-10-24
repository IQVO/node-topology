/** Wire types of apis/openapi.yaml (camelCase, as the REST contract names them). */

export const NODE_TYPES = [
  "FULFILLMENT_CENTER",
  "INBOUND_CROSS_DOCK",
  "RETURNS_CENTER",
  "SORTATION_CENTER",
  "DELIVERY_STATION",
  "AIR_HUB",
] as const;
export type NodeType = (typeof NODE_TYPES)[number];

export const NODE_STATUSES = ["PROPOSED", "ACTIVE", "SUSPENDED", "RETIRED"] as const;
export type NodeStatus = (typeof NODE_STATUSES)[number];

export const LANE_MODES = ["GROUND", "AIR"] as const;
export type LaneMode = (typeof LANE_MODES)[number];

export interface VendorRef {
  vendorId: string;
  vendorNodeCode: string;
}

export interface GeoLocation {
  postalCode: string;
  countryCode: string;
  lat?: number;
  lon?: number;
}

export interface Capabilities {
  outbound: boolean;
  inbound: boolean;
  returns: boolean;
  sort: boolean;
  lastMile: boolean;
  air: boolean;
}

export interface DayHours {
  start: string;
  end: string;
}

export interface CutoffSlot {
  localTime: string;
  days?: string[];
  maxOrders?: number;
}

export interface ThroughputCap {
  value: number;
  timeUnit: string;
  unit: string;
}

export interface OperationalProfile {
  profileRevision: number;
  timezone: string;
  operatingHoursByDay: Record<string, DayHours>;
  holidays: string[];
  handlingTimeMinutes: number;
  cutoffs: CutoffSlot[];
  throughputCap: ThroughputCap;
  sortDwellMinutes?: number;
  declaredBy: string;
  declaredAt: string;
}

export interface Node {
  nodeId: string;
  nodeType: NodeType;
  subtype?: string;
  operator: string;
  vendorRef?: VendorRef;
  regionId: string;
  timezone: string;
  location: GeoLocation;
  capabilities: Capabilities;
  status: NodeStatus;
  operationalProfile?: OperationalProfile;
  activatedAt?: string;
  suspendedAt?: string;
  suspendReason?: string;
  retiredAt?: string;
}

export interface NodeList {
  nodes: Node[];
}

/** GET /nodes?type=&region=&status= */
export interface ListNodesQuery {
  type?: NodeType;
  regionId?: string;
  status?: NodeStatus;
}

export interface Lane {
  laneId: string;
  originNodeId: string;
  destinationNodeId: string;
  mode: LaneMode;
  nominalTransitMinutes: number;
  status: string;
  closedAt?: string;
}

export interface LaneList {
  lanes: Lane[];
}

/** GET /lanes?origin=&destination= */
export interface ListLanesQuery {
  origin?: string;
  destination?: string;
}

export interface ServiceArea {
  postalPrefix: string;
  deliveryStationNodeId: string;
  regionId: string;
  assignmentVersion: number;
}

export interface Region {
  regionId: string;
  code: string;
  name: string;
}
