import { describe, it, expect } from "vitest";
import {
  snapshotOf,
  isStale,
  markSolved,
  visibleResult,
  canDrawTimeline,
  canShowCycle,
} from "./state.js";

const services = [
  { id: "a", lo: 0, hi: 0 },
  { id: "b", lo: -10, hi: 10 },
];
const spans = [
  { id: "s1", service: "a", parent: "", start: 0, end: 100 },
  { id: "s2", service: "b", parent: "s1", start: -3, end: 0 },
];

const okResult = {
  status: "ok",
  order: ["a", "b"],
  offsets: { a: 0, b: 3 },
  spans: [
    {
      id: "s1",
      service: "a",
      parent: "",
      start: 0,
      end: 100,
      correctedStart: 0,
      correctedEnd: 100,
    },
    {
      id: "s2",
      service: "b",
      parent: "s1",
      start: -3,
      end: 0,
      correctedStart: 0,
      correctedEnd: 3,
    },
  ],
  constraints: [{ span: "s2", parentSpan: "s1", startSlack: 0, endSlack: 97 }],
};

const infeasibleResult = {
  status: "infeasible",
  cycle: {
    totalWeight: -2,
    edges: [{ from: "a", to: "b", weight: -2, ref: { kind: "child_end" } }],
  },
};

describe("结果失效", () => {
  it("求解成功后立即可见", () => {
    const snap = markSolved(services, spans);
    expect(isStale(snap, services, spans)).toBe(false);
    expect(visibleResult(okResult, false)).toBe(okResult);
  });

  it("编辑偏移界后旧结果立即失效", () => {
    const snap = markSolved(services, spans);
    const edited = services.map((s) => (s.id === "b" ? { ...s, hi: 5 } : s));
    expect(isStale(snap, edited, spans)).toBe(true);
    const stale = true;
    expect(visibleResult(okResult, stale)).toBeNull();
    expect(canDrawTimeline(okResult, stale)).toBe(false);
    expect(canShowCycle(okResult, stale)).toBe(false);
  });

  it("编辑 span 后旧结果立即失效", () => {
    const snap = markSolved(services, spans);
    const edited = spans.map((s) => (s.id === "s2" ? { ...s, start: -4 } : s));
    expect(isStale(snap, services, edited)).toBe(true);
  });

  it("新增 span 也会失效", () => {
    const snap = markSolved(services, spans);
    const edited = [
      ...spans,
      { id: "s3", service: "b", parent: "s1", start: 1, end: 2 },
    ];
    expect(isStale(snap, services, edited)).toBe(true);
  });

  it("重新求解后结果恢复可见", () => {
    const edited = services.map((s) => (s.id === "b" ? { ...s, hi: 5 } : s));
    const snap2 = markSolved(edited, spans);
    expect(isStale(snap2, edited, spans)).toBe(false);
    expect(canDrawTimeline(okResult, false)).toBe(true);
  });
});

describe("时间线绘制门禁", () => {
  it("ok 结果可绘制", () => {
    expect(canDrawTimeline(okResult, false)).toBe(true);
    expect(canShowCycle(okResult, false)).toBe(false);
  });

  it("infeasible 结果只展示矛盾环，绝不绘制时间线", () => {
    expect(canDrawTimeline(infeasibleResult, false)).toBe(false);
    expect(canShowCycle(infeasibleResult, false)).toBe(true);
  });

  it("无 span 的 ok 结果不绘制空时间线", () => {
    expect(canDrawTimeline({ status: "ok", spans: [] }, false)).toBe(false);
  });

  it("无结果时什么都不展示", () => {
    expect(canDrawTimeline(null, false)).toBe(false);
    expect(canShowCycle(null, false)).toBe(false);
  });
});

describe("快照语义", () => {
  it("快照与键序无关地反映内容", () => {
    expect(snapshotOf(services, spans)).toBe(snapshotOf(services, spans));
    expect(snapshotOf(services, spans)).not.toBe(snapshotOf(services, []));
  });
});
