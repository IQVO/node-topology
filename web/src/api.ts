import { TOPOLOGY_API_BASE } from "./config";
import type {
  Lane, LaneList, ListLanesQuery, ListNodesQuery, Node, NodeList, Region, ServiceArea,
} from "./types";

/** RFC 9457 problem+json body every error response from node-topology
 *  returns (`type` = .../<slug>, e.g. not-found, validation-failed). */
export interface ProblemDetails {
  type?: string;
  title?: string;
  status?: number;
  detail?: string;
  instance?: string;
}

/**
 * Every failed call -- an HTTP error with a problem+json body, an HTTP error
 * without one, or a network failure -- surfaces as one ApiError, so a screen
 * has a single shape to render: `title` (the problem's category) and `detail`
 * (what exactly was wrong), plus `slug` to branch on a specific problem
 * (e.g. `not-found`).
 */
export class ApiError extends Error {
  readonly status: number;
  readonly problem: ProblemDetails | null;
  readonly title: string;
  readonly detail: string;
  /** Last path segment of problem.type (e.g. "not-found"); "" when unknown. */
  readonly slug: string;

  constructor(status: number, problem: ProblemDetails | null, fallbackTitle: string) {
    const title = problem?.title || fallbackTitle;
    const detail = problem?.detail ?? "";
    super(detail || title);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
    this.title = title;
    this.detail = detail;
    this.slug = problem?.type ? (problem.type.split("/").pop() ?? "") : "";
  }
}

/** Normalizes anything thrown by a request into an ApiError. */
export function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) return err;
  const message = err instanceof Error ? err.message : String(err);
  return new ApiError(0, { detail: message }, "Network error");
}

type Query = Record<string, string | number | boolean | undefined>;

export function withQuery(path: string, query?: Query): string {
  if (!query) return path;
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

/**
 * The one door to the service. Only this context's own API is ever called
 * (`TOPOLOGY_API_BASE` = <apiOrigin>/api/node-topology): no sibling context.
 * This remote is read-only, so only GETs exist here; they send no custom
 * header, so they stay simple CORS requests.
 */
async function request<T>(path: string): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${TOPOLOGY_API_BASE}${path}`);
  } catch (err) {
    throw toApiError(err);
  }
  if (!res.ok) {
    let problem: ProblemDetails | null = null;
    try {
      problem = (await res.json()) as ProblemDetails;
    } catch {
      // non-JSON error body -- fall through with problem = null
    }
    throw new ApiError(res.status, problem, `${res.status} ${res.statusText}`.trim());
  }
  return (await res.json()) as T;
}

const seg = encodeURIComponent;

/** One page of a collection: `{ items, nextCursor }`, nextCursor absent on the last page. */
interface Page<T> {
  items?: T[];
  nextCursor?: string;
}

/** Hard bound on the pages followed for one list (500 rows each), so a broken cursor cannot loop forever. */
const MAX_PAGES = 100;

/** Reads every page of a collection: follows `nextCursor` until it is absent. */
async function collect<T>(path: string, query: Record<string, string | undefined>): Promise<T[]> {
  const all: T[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < MAX_PAGES; page++) {
    const res = await request<Page<T>>(withQuery(path, { ...query, limit: 500, cursor }));
    all.push(...(res.items ?? []));
    if (!res.nextCursor) return all;
    cursor = res.nextCursor;
  }
  throw new ApiError(0, null, `${path}: more than ${MAX_PAGES} pages`);
}

/** GET /nodes?type=&region=&status= -- the registry, every page, id ascending. */
export async function listNodes(q: ListNodesQuery): Promise<NodeList> {
  return { nodes: await collect<Node>("/nodes", { type: q.type, region: q.regionId, status: q.status }) };
}

/** GET /nodes/{nodeId} (404 not-found). */
export async function getNode(nodeId: string): Promise<Node> {
  return request<Node>(`/nodes/${seg(nodeId)}`);
}

/** GET /lanes?origin=&destination= -- open and closed lanes together, every page. */
export async function listLanes(q: ListLanesQuery): Promise<LaneList> {
  return { lanes: await collect<Lane>("/lanes", { origin: q.origin, destination: q.destination }) };
}

/** GET /service-areas/{postalPrefix} -- which delivery station serves a prefix. */
export async function resolveServiceArea(postalPrefix: string): Promise<ServiceArea> {
  return request<ServiceArea>(`/service-areas/${seg(postalPrefix)}`);
}

/** GET /regions -- every region, every page, id ascending. */
export async function listRegions(): Promise<Region[]> {
  return collect<Region>("/regions", {});
}

/** GET /regions/{regionId} (404 not-found). */
export async function getRegion(regionId: string): Promise<Region> {
  return request<Region>(`/regions/${seg(regionId)}`);
}
