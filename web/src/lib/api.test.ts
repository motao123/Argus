import { describe, expect, it } from "vitest";
import { notificationUpdatePayload } from "./api";

describe("notificationUpdatePayload", () => {
  it("does not write redacted or blank sensitive values back", () => {
    expect(notificationUpdatePayload({ id: 1, name: "renamed", url: "https://example.com/***", headers: "", body: "" })).toEqual({ id: 1, name: "renamed" });
  });

  it("supports explicit replace and clear operations", () => {
    expect(notificationUpdatePayload({ id: 1, url: "https://new.example/hook", clear_headers: true, body: "new body" })).toEqual({
      id: 1,
      url: "https://new.example/hook",
      clear_headers: true,
      body: "new body",
    });
  });
});
