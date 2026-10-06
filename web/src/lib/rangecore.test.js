import { describe, it, expect } from "vitest";
import {
  markSolved,
  isStale,
  visibleResult,
  canDownload,
  parseRequest,
  describePoint,
  formatWitness,
  verifyRanges,
  describeEdge,
  nodeName,
} from "./rangecore.js";

// 与后端求解器输出一致的请求/响应样例：
// x_b 被父子链约束到 [-2,2]；q-chain=[0,4]，q-free=[-1,3]，同服务恒为 1。
const request = {
  input: {
    services: [
      { id: "a", lo: 0, hi: 0 },
      { id: "b", lo: -10, hi: 10 },
    ],
    spans: [
      { id: "p", service: "a", parent: "", start: 0, end: 10 },
      { id: "c", service: "b", parent: "p", start: 2, end: 8 },
      { id: "u", service: "b", parent: "", start: 1, end: 3 },
    ],
  },
  queries: [
    { id: "q-chain", left: { span: "p", point: "start" }, right: { span: "c", point: "start" } },
    { id: "q-free", left: { span: "p", point: "start" }, right: { span: "u", point: "start" } },
    { id: "q-same", left: { span: "c", point: "start" }, right: { span: "u", point: "end" } },
  ],
};

const response = {
  base: { status: "ok", order: ["a", "b"], offsets: { a: 0, b: -2 } },
  ranges: [
    {
      id: "q-chain",
      left: { span: "p", point: "start" },
      right: { span: "c", point: "start" },
      min: 0,
      max: 4,
      minWitness: { a: 0, b: -2 },
      maxWitness: { a: 0, b: 2 },
    },
    {
      id: "q-free",
      left: { span: "p", point: "start" },
      right: { span: "u", point: "start" },
      min: -1,
      max: 3,
      minWitness: { a: 0, b: -2 },
      maxWitness: { a: 0, b: 2 },
    },
    {
      id: "q-same",
      left: { span: "c", point: "start" },
      right: { span: "u", point: "end" },
      min: 1,
      max: 1,
      minWitness: { a: 0, b: 2 },
      maxWitness: { a: 0, b: 2 },
    },
  ],
};

const requestText = JSON.stringify(request, null, 2);

describe("结果失效", () => {
  it("复核成功后立即可见、可下载", () => {
    const snap = markSolved(requestText);
    expect(isStale(snap, requestText)).toBe(false);
    expect(visibleResult(response, false)).toBe(response);
    expect(canDownload(response, false)).toBe(true);
  });

  it("编辑请求后旧范围与下载立即失效", () => {
    const snap = markSolved(requestText);
    const edited = requestText.replace('"hi": 10', '"hi": 9');
    expect(isStale(snap, edited)).toBe(true);
    expect(visibleResult(response, true)).toBeNull();
    expect(canDownload(response, true)).toBe(false);
  });

  it("文本还原后结果恢复有效", () => {
    const snap = markSolved(requestText);
    expect(isStale(snap, requestText)).toBe(false);
  });

  it("尚未复核时不存在失效概念", () => {
    expect(isStale(null, requestText)).toBe(false);
    expect(canDownload(null, false)).toBe(false);
  });
});

describe("请求解析", () => {
  it("合法 JSON 对象", () => {
    expect(parseRequest(requestText).value).toEqual(request);
  });
  it("非法 JSON", () => {
    expect(parseRequest("{oops").error).toMatch(/JSON/);
  });
  it("非对象", () => {
    expect(parseRequest("[1,2]").error).toBeTruthy();
    expect(parseRequest("3").error).toBeTruthy();
  });
});

describe("范围独立复验", () => {
  it("忠实响应通过复验", () => {
    expect(verifyRanges(request, response)).toEqual([]);
  });

  it("见证未达到声明端点被检出", () => {
    const bad = structuredClone(response);
    bad.ranges[0].minWitness = { a: 0, b: 0 }; // 实际差值 2，不是 min=0
    expect(verifyRanges(request, bad).join("\n")).toMatch(/未达到声明端点/);
  });

  it("见证缺服务被检出", () => {
    const bad = structuredClone(response);
    bad.ranges[0].maxWitness = { a: 0 };
    expect(verifyRanges(request, bad).join("\n")).toMatch(/未恰好覆盖全部服务/);
  });

  it("见证超出偏移界被检出", () => {
    const bad = structuredClone(response);
    bad.ranges[0].maxWitness = { a: 0, b: 50 };
    expect(verifyRanges(request, bad).join("\n")).toMatch(/超出偏移界/);
  });

  it("见证违反父子约束被检出", () => {
    const bad = structuredClone(response);
    bad.ranges[0].maxWitness = { a: 0, b: 5 }; // 界内但 c 终点越出 p
    expect(verifyRanges(request, bad).join("\n")).toMatch(/违反父子约束/);
  });

  it("min 大于 max 被检出", () => {
    const bad = structuredClone(response);
    bad.ranges[1].min = 5;
    bad.ranges[1].minWitness = { a: 0, b: 2 }; // 同时也不再达到端点
    const problems = verifyRanges(request, bad).join("\n");
    expect(problems).toMatch(/min 5 大于 max 3/);
  });

  it("结果顺序被打乱被检出", () => {
    const bad = structuredClone(response);
    [bad.ranges[0], bad.ranges[1]] = [bad.ranges[1], bad.ranges[0]];
    expect(verifyRanges(request, bad).join("\n")).toMatch(/不一致/);
  });

  it("范围数量不符被检出", () => {
    const bad = structuredClone(response);
    bad.ranges.pop();
    expect(verifyRanges(request, bad).join("\n")).toMatch(/数量/);
  });

  it("左右时刻被改动被检出", () => {
    const bad = structuredClone(response);
    bad.ranges[0].right = { span: "u", point: "start" };
    expect(verifyRanges(request, bad).join("\n")).toMatch(/被改动/);
  });

  it("非 ok 响应无法复验", () => {
    expect(verifyRanges(request, { base: { status: "infeasible" } })[0]).toMatch(/不是 ok/);
  });
});

describe("格式化", () => {
  it("时刻点描述", () => {
    expect(describePoint({ span: "s1", point: "start" })).toBe("s1.start");
  });
  it("见证按给定顺序格式化", () => {
    expect(formatWitness({ a: 0, b: -2 }, ["a", "b"])).toBe("a:+0  b:-2");
  });
  it("矛盾环边描述", () => {
    expect(describeEdge({ ref: { kind: "bound_lower", service: "b" } })).toMatch(/偏移下界/);
    expect(describeEdge({ ref: { kind: "child_end", span: "c", parentSpan: "p" } })).toMatch(/终点/);
    expect(nodeName("@zero")).toBe("零点基准");
    expect(nodeName("a")).toBe("a");
  });
});
