import { describe, expect, it } from "vitest";
import { resolveTopologyApiBase } from "./config";

describe("resolveTopologyApiBase", () => {
  it("appends this context's API path to the runtime apiOrigin", () => {
    expect(resolveTopologyApiBase({ apiOrigin: "http://localhost:8000" }, true)).toBe(
      "http://localhost:8000/api/node-topology",
    );
  });

  it("trims trailing slashes off the origin", () => {
    expect(resolveTopologyApiBase({ apiOrigin: "http://localhost:8000/" }, false)).toBe(
      "http://localhost:8000/api/node-topology",
    );
  });

  it("falls back to the local api port in development", () => {
    expect(resolveTopologyApiBase({}, false)).toBe("http://localhost:8080");
  });

  it("refuses to boot without an origin in production", () => {
    expect(() => resolveTopologyApiBase({}, true)).toThrow(/apiOrigin is required/);
  });
});
