import { describe, expect, it } from "vitest";
import { capabilityWords, locationLabel, minutesLabel, typeLabel, utcLabel } from "./lib";

describe("utcLabel", () => {
  it("renders an RFC 3339 instant as a compact UTC stamp", () => {
    expect(utcLabel("2026-10-20T09:15:00.500Z")).toBe("2026-10-20 09:15:00Z");
  });
  it("passes unparseable text through", () => {
    expect(utcLabel("whenever")).toBe("whenever");
  });
});

describe("minutesLabel", () => {
  it("shows minutes under two hours", () => {
    expect(minutesLabel(90)).toBe("90 min");
  });
  it("collapses whole hours", () => {
    expect(minutesLabel(240)).toBe("4 h");
  });
});

describe("typeLabel", () => {
  it("spells a node type out", () => {
    expect(typeLabel("FULFILLMENT_CENTER")).toBe("fulfillment centre");
    expect(typeLabel("AIR_HUB")).toBe("air hub");
  });
  it("passes unknown codes through", () => {
    expect(typeLabel("MYSTERY")).toBe("MYSTERY");
  });
});

describe("locationLabel", () => {
  it("shows postal code and country, with coordinates when known", () => {
    expect(locationLabel({ postalCode: "01310", countryCode: "BR" })).toBe("01310, BR");
    expect(locationLabel({ postalCode: "01310", countryCode: "BR", lat: -23.5614, lon: -46.6559 })).toBe(
      "01310, BR (-23.5614, -46.6559)",
    );
  });
});

describe("capabilityWords", () => {
  it("lists the capabilities that are on", () => {
    expect(
      capabilityWords({ outbound: true, inbound: false, returns: false, sort: true, lastMile: false, air: false }),
    ).toEqual(["outbound", "sort"]);
  });
});
