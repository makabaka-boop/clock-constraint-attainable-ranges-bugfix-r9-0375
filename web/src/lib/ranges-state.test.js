import { describe, it, expect } from "vitest";
import {
  isRangesStale,
  markRangesSolved,
  visibleRangesResult,
  canShowRanges,
  canDownloadRanges,
  canShowRangesCycle,
} from "./ranges-state.js";

const req = JSON.stringify({ input: { services: [], spans: [] }, queries: [] });

const okResult = {
  base: { status: "ok", offsets: { a: 0 } },
  ranges: [{ id: "q", left: { span: "p", point: "start" }, right: { span: "c", point: "start" }, min: 0, max: 10,
    minWitness: { a: 0, b: 0 }, maxWitness: { a: 0, b: 10 } }],
};

const infeasibleResult = {
  base: { status: "infeasible", cycle: { totalWeight: -2, edges: [] } },
};

describe("范围结果失效", () => {
  it("复核成功后立即可见且可下载", () => {
    const snap = markRangesSolved(req);
    expect(isRangesStale(snap, req)).toBe(false);
    expect(canShowRanges(okResult, false)).toBe(true);
    expect(canDownloadRanges(okResult, false)).toBe(true);
  });

  it("编辑输入后旧范围立即失效且禁止下载", () => {
    const snap = markRangesSolved(req);
    expect(isRangesStale(snap, req + " ")).toBe(true);
    expect(visibleRangesResult(okResult, true)).toBeNull();
    expect(canShowRanges(okResult, true)).toBe(false);
    expect(canDownloadRanges(okResult, true)).toBe(false);
  });

  it("重新复核后恢复", () => {
    const edited = req + " ";
    const snap2 = markRangesSolved(edited);
    expect(isRangesStale(snap2, edited)).toBe(false);
    expect(canDownloadRanges(okResult, false)).toBe(true);
  });

  it("无结果时什么都不展示、不可下载", () => {
    expect(canShowRanges(null, false)).toBe(false);
    expect(canDownloadRanges(null, false)).toBe(false);
    expect(canShowRangesCycle(null, false)).toBe(false);
  });
});

describe("无解只展示矛盾环", () => {
  it("无解结果不可下载范围证据", () => {
    expect(canDownloadRanges(infeasibleResult, false)).toBe(false);
    expect(canShowRangesCycle(infeasibleResult, false)).toBe(true);
    expect(canShowRangesCycle(okResult, false)).toBe(false);
  });

  it("编辑后矛盾环也失效", () => {
    expect(canShowRangesCycle(infeasibleResult, true)).toBe(false);
  });
});
