import { describe, expect, it } from "vitest";

import type { Server } from "../lib/api";
import { aggregateServersByCountry } from "./WorldMap";

function server(id: number, countryCode: string | undefined, online: boolean): Server {
  return {
    id,
    name: `node-${id}`,
    online,
    host: countryCode ? { country_code: countryCode } : undefined,
  } as Server;
}

describe("aggregateServersByCountry", () => {
  it("normalizes country codes and aggregates online servers", () => {
    const points = aggregateServersByCountry([
      server(1, "cn", true),
      server(2, " CN ", false),
      server(3, "US", true),
    ]);

    expect(points).toHaveLength(2);
    expect(points.find((point) => point.country === "CN")).toMatchObject({ online: 1, total: 2 });
    expect(points.find((point) => point.country === "US")).toMatchObject({ online: 1, total: 1 });
  });

  it("skips missing and unknown country codes", () => {
    expect(aggregateServersByCountry([
      server(1, undefined, true),
      server(2, "ZZ", true),
    ])).toEqual([]);
  });

  it("limits tooltip names to three servers", () => {
    const points = aggregateServersByCountry([
      server(1, "DE", true),
      server(2, "DE", true),
      server(3, "DE", true),
      server(4, "DE", true),
    ]);

    expect(points[0].names).toEqual(["node-1", "node-2", "node-3"]);
    expect(points[0].total).toBe(4);
  });
});
