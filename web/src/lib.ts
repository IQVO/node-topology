import { NO_VALUE } from "@warehouse/ui-kit";
import type { Capabilities } from "./types";

/** "2026-10-09 08:05:30Z" for an RFC 3339 instant; the raw text when unparseable. */
export function utcLabel(iso: string | undefined): string {
  if (!iso) return NO_VALUE;
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const d = new Date(t).toISOString();
  return `${d.slice(0, 10)} ${d.slice(11, 19)}Z`;
}

/** Whole minutes with a unit: "240 min"; 90 minutes reads "1.5 h" past an hour. */
export function minutesLabel(min: number): string {
  return min >= 120 && min % 60 === 0 ? `${min / 60} h` : `${min} min`;
}

/** The human word of a node type code: FULFILLMENT_CENTER -> fulfillment centre. */
export function typeLabel(code: string): string {
  const words: Record<string, string> = {
    FULFILLMENT_CENTER: "fulfillment centre",
    INBOUND_CROSS_DOCK: "inbound cross-dock",
    RETURNS_CENTER: "returns centre",
    SORTATION_CENTER: "sortation centre",
    DELIVERY_STATION: "delivery station",
    AIR_HUB: "air hub",
  };
  return words[code] ?? code;
}

/** "01310, BR" (with lat/lon when known) for a node location. */
export function locationLabel(loc: { postalCode: string; countryCode: string; lat?: number; lon?: number }): string {
  const base = `${loc.postalCode}, ${loc.countryCode}`;
  if (loc.lat === undefined || loc.lon === undefined) return base;
  return `${base} (${loc.lat.toFixed(4)}, ${loc.lon.toFixed(4)})`;
}

/** The capabilities a node has, as words; a node with none shows "none". */
export function capabilityWords(c: Capabilities): string[] {
  const words: Record<string, string> = {
    outbound: "outbound",
    inbound: "inbound",
    returns: "returns",
    sort: "sort",
    lastMile: "last mile",
    air: "air",
  };
  return Object.entries(words)
    .filter(([key]) => c[key as keyof Capabilities])
    .map(([, word]) => word);
}
