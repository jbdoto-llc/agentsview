import { describe, expect, it } from "vite-plus/test";
import { formatUSD, prLabel, stateTone, totalTokens, traceHref } from "./stories.js";

describe("formatUSD", () => {
  it.each([
    [0.11270399999999998, "$0.11"],
    [1.5, "$1.50"],
    [0.004, "<$0.01"],
    [0, "$0.00"],
    [undefined, "$0.00"],
    [Number.NaN, "$0.00"],
  ])("formats %s as %s", (value, want) => {
    expect(formatUSD(value)).toBe(want);
  });
});

describe("traceHref", () => {
  it("fills every placeholder and escapes the id", () => {
    expect(traceHref("https://traces.example.test/{trace_id}?q={trace_id}", "a b")).toBe(
      "https://traces.example.test/a%20b?q=a%20b",
    );
  });
  it.each([
    [undefined, "abc"],
    ["https://traces.example.test/", "abc"],
    ["https://traces.example.test/{trace_id}", undefined],
    ["", ""],
  ])("returns null for template %s and id %s", (template, id) => {
    expect(traceHref(template, id)).toBeNull();
  });
});

describe("prLabel", () => {
  it.each([
    ["https://example.test/org/repo/pull/552", "#552"],
    ["https://example.test/org/repo/pull/7/files", "#7"],
    ["https://example.test/other", "https://example.test/other"],
    [undefined, ""],
  ])("labels %s as %s", (url, want) => {
    expect(prLabel(url)).toBe(want);
  });
});

describe("stateTone", () => {
  it.each([
    ["running", "running"],
    ["claimed", "running"],
    ["pr_open", "ok"],
    ["closed", "ok"],
    ["failed", "failed"],
    ["open", "muted"],
    [undefined, "muted"],
  ])("maps %s to %s", (state, want) => {
    expect(stateTone(state)).toBe(want);
  });
});

describe("totalTokens", () => {
  it("sums every kind and tolerates gaps", () => {
    expect(totalTokens({ input: 6, output: 343, cache_read: 43470, cache_creation: 6901 })).toBe(
      50720,
    );
    expect(totalTokens({ output: 5 })).toBe(5);
    expect(totalTokens(undefined)).toBe(0);
  });
});
