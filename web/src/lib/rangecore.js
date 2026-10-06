// 时刻差范围复核页的纯逻辑：请求快照失效判定、响应独立复验、展示格式化。
// 不依赖 DOM，供页面（src/ranges/main.js）与测试共用。
//
// 核心约定：复核成功时记录请求原文快照；此后请求文本有任何变化，旧范围
// 与下载证据立即失效——页面隐藏结果并禁用下载，直到重新复核。

export function markSolved(requestText) {
  return requestText;
}

export function isStale(solvedSnapshot, requestText) {
  return solvedSnapshot !== null && solvedSnapshot !== requestText;
}

// 只有“未过期的结果”才可见、可下载。
export function visibleResult(result, stale) {
  return stale ? null : result;
}

export function canDownload(result, stale) {
  return visibleResult(result, stale) !== null;
}

export function parseRequest(text) {
  let v;
  try {
    v = JSON.parse(text);
  } catch (e) {
    return { error: "JSON 解析失败: " + e.message };
  }
  if (v === null || typeof v !== "object" || Array.isArray(v)) {
    return { error: "请求必须是 JSON 对象（含 input 与 queries）" };
  }
  return { value: v };
}

export function describePoint(p) {
  return `${p.span}.${p.point}`;
}

export function formatWitness(w, order) {
  const ids = order && order.length ? order : Object.keys(w).sort();
  return ids.map((id) => `${id}:${w[id] >= 0 ? "+" : ""}${w[id]}`).join("  ");
}

// verifyRanges 不依赖后端结论，从请求与响应原文独立复验每个范围：
// 结果数量、ID、左右时刻与查询一一对应；min <= max；每份见证覆盖全部
// 服务、落在各自偏移界内、满足全部父子约束，且确实达到声明的端点。
// 返回问题列表，为空表示复验通过。
export function verifyRanges(request, response) {
  const problems = [];
  if (!response || typeof response !== "object") {
    return ["响应不是 JSON 对象"];
  }
  const base = response.base;
  if (!base || base.status !== "ok") {
    return ["base 状态不是 ok，无法复验范围"];
  }
  const input = request.input || {};
  const services = new Map((input.services || []).map((s) => [s.id, s]));
  const spans = new Map((input.spans || []).map((sp) => [sp.id, sp]));
  const queries = Array.isArray(request.queries) ? request.queries : [];
  const ranges = Array.isArray(response.ranges) ? response.ranges : [];
  if (ranges.length !== queries.length) {
    problems.push(`范围数量 ${ranges.length} 与查询数量 ${queries.length} 不一致`);
  }

  const localOf = (p) => {
    const sp = spans.get(p.span);
    return p.point === "start" ? sp.start : sp.end;
  };
  const checkWitness = (qid, label, w, endpoint, q) => {
    if (!w || typeof w !== "object" || Array.isArray(w)) {
      problems.push(`查询 ${qid} 的 ${label} 不是偏移表`);
      return;
    }
    const missing = [...services.keys()].filter((id) => !(id in w));
    const extra = Object.keys(w).filter((id) => !services.has(id));
    if (missing.length || extra.length) {
      problems.push(
        `查询 ${qid} 的 ${label} 未恰好覆盖全部服务（缺 ${missing.join(",") || "无"}，多 ${extra.join(",") || "无"}）`
      );
    }
    for (const [id, svc] of services) {
      const v = w[id];
      if (typeof v !== "number" || !Number.isInteger(v)) {
        problems.push(`查询 ${qid} 的 ${label} 中 ${id} 的偏移不是整数`);
        continue;
      }
      if (v < svc.lo || v > svc.hi) {
        problems.push(`查询 ${qid} 的 ${label} 中 ${id}=${v} 超出偏移界 [${svc.lo},${svc.hi}]`);
      }
    }
    for (const sp of input.spans || []) {
      if (!sp.parent) continue;
      const p = spans.get(sp.parent);
      if (!p) continue;
      const cs = w[sp.service];
      const ps = w[p.service];
      if (typeof cs !== "number" || typeof ps !== "number") continue;
      if (sp.start + cs < p.start + ps) {
        problems.push(`查询 ${qid} 的 ${label} 违反父子约束：${sp.id} 起点早于其父 ${p.id} 起点`);
      }
      if (sp.end + cs > p.end + ps) {
        problems.push(`查询 ${qid} 的 ${label} 违反父子约束：${sp.id} 终点晚于其父 ${p.id} 终点`);
      }
    }
    const ls = spans.get(q.left.span);
    const rs = spans.get(q.right.span);
    if (!ls || !rs) return; // 请求本身非法，由后端整份拒绝
    const achieved = localOf(q.right) + w[rs.service] - (localOf(q.left) + w[ls.service]);
    if (achieved !== endpoint) {
      problems.push(`查询 ${qid} 的 ${label} 实际差值 ${achieved} 未达到声明端点 ${endpoint}`);
    }
  };

  const n = Math.min(ranges.length, queries.length);
  for (let i = 0; i < n; i++) {
    const q = queries[i];
    const r = ranges[i];
    if (r.id !== q.id) {
      problems.push(`第 ${i + 1} 个结果 ID「${r.id}」与查询 ID「${q.id}」不一致`);
    }
    if (
      JSON.stringify(r.left) !== JSON.stringify(q.left) ||
      JSON.stringify(r.right) !== JSON.stringify(q.right)
    ) {
      problems.push(`查询 ${q.id} 的左右时刻在结果中被改动`);
    }
    if (!(r.min <= r.max)) {
      problems.push(`查询 ${q.id} 的 min ${r.min} 大于 max ${r.max}`);
    }
    checkWitness(q.id, "minWitness", r.minWitness, r.min, q);
    checkWitness(q.id, "maxWitness", r.maxWitness, r.max, q);
  }
  return problems;
}

// 矛盾环展示（与主页面一致的措辞）。
export function describeEdge(e) {
  const ref = e.ref || {};
  switch (ref.kind) {
    case "bound_upper":
      return `服务 ${ref.service} 的偏移上界（x ≤ hi）`;
    case "bound_lower":
      return `服务 ${ref.service} 的偏移下界（x ≥ lo）`;
    case "child_start":
      return `子 span ${ref.span} 起点须不早于父 span ${ref.parentSpan} 起点`;
    case "child_end":
      return `子 span ${ref.span} 终点须不晚于父 span ${ref.parentSpan} 终点`;
    default:
      return "未知约束";
  }
}

export const nodeName = (n) => (n === "@zero" ? "零点基准" : n);
