// 时刻差范围复核页的 DOM 绑定。所有可判定逻辑都在 lib/rangecore.js，
// 这里只做事件绑定与渲染。
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
} from "../lib/rangecore.js";

const $ = (id) => document.getElementById(id);

// 默认示例覆盖三种典型情形：链上查询被调用约束收紧、自由 span 经服务
// 偏移被间接约束、同服务查询差值为常数。
const DEFAULT_REQUEST = {
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
    { id: "q-same-service", left: { span: "c", point: "start" }, right: { span: "u", point: "end" } },
  ],
};

let solvedSnapshot = null; // 复核成功时的请求原文
let solvedRequest = null; // 复核成功时解析后的请求对象（供独立复验）
let result = null; // 最近一次成功响应（即下载内容）

$("request").value = JSON.stringify(DEFAULT_REQUEST, null, 2);

// 输入一旦变化，快照失配：旧范围与下载证据立即失效（隐藏结果、禁用下载）。
function refresh() {
  const stale = isStale(solvedSnapshot, $("request").value);
  const visible = visibleResult(result, stale);
  $("stale").hidden = !stale;
  $("ok").hidden = !(visible && visible.base && visible.base.status === "ok");
  $("cycle").hidden = !(visible && visible.base && visible.base.status === "infeasible");
  $("raw").hidden = visible === null;
  $("download").disabled = !canDownload(result, stale);
}

$("request").addEventListener("input", refresh);

function showErrors(list) {
  const ul = $("errorList");
  ul.replaceChildren();
  for (const msg of list) {
    const li = document.createElement("li");
    li.textContent = msg;
    ul.appendChild(li);
  }
  $("errors").hidden = list.length === 0;
}

function renderCycle(cycle) {
  $("cycleTotal").textContent = cycle ? cycle.totalWeight : "?";
  const ol = $("cycleEdges");
  ol.replaceChildren();
  for (const e of (cycle && cycle.edges) || []) {
    const li = document.createElement("li");
    const path = document.createElement("code");
    path.textContent = `${nodeName(e.from)} → ${nodeName(e.to)}`;
    const w = document.createElement("span");
    w.className = "w";
    w.textContent = ` 权重 ${e.weight} `;
    li.append(path, w, document.createTextNode("— " + describeEdge(e)));
    ol.appendChild(li);
  }
}

function renderRanges() {
  // 独立复验：不采信后端结论，从请求与响应原文重算见证是否真实达到端点。
  const problems = verifyRanges(solvedRequest, result);
  const v = $("verify");
  if (problems.length === 0) {
    v.textContent = `独立复验通过：${(result.ranges || []).length} 个查询的见证均满足全部约束且达到声明端点。`;
    v.className = "hint ok-text";
  } else {
    v.textContent = "复验发现问题：" + problems.join("；");
    v.className = "hint bad-text";
  }
  const order = result.base.order || [];
  const tbody = $("rangeRows");
  tbody.replaceChildren();
  for (const r of result.ranges || []) {
    const tr = document.createElement("tr");
    const cells = [
      r.id,
      describePoint(r.left),
      describePoint(r.right),
      String(r.min),
      String(r.max),
      formatWitness(r.minWitness, order),
      formatWitness(r.maxWitness, order),
    ];
    cells.forEach((text, i) => {
      const td = document.createElement("td");
      td.textContent = text;
      if (i === 3 || i === 4) td.className = "num";
      if (i >= 5) td.className = "wit";
      tr.appendChild(td);
    });
    tbody.appendChild(tr);
  }
}

async function run() {
  showErrors([]);
  const parsed = parseRequest($("request").value);
  if (parsed.error) {
    showErrors([parsed.error]);
    return;
  }
  $("run").disabled = true;
  try {
    const resp = await fetch("/api/ranges", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: $("request").value,
    });
    const data = await resp.json().catch(() => null);
    if (!resp.ok) {
      showErrors((data && data.errors) || [`HTTP ${resp.status}`]);
      result = null;
      solvedSnapshot = null;
      solvedRequest = null;
      refresh();
      return;
    }
    result = data;
    solvedRequest = parsed.value;
    solvedSnapshot = markSolved($("request").value);
    $("rawJson").textContent = JSON.stringify(result, null, 2);
    if (result.base && result.base.status === "infeasible") {
      renderCycle(result.base.cycle);
    } else {
      renderRanges();
    }
    refresh();
  } catch (e) {
    showErrors(["网络错误: " + ((e && e.message) || e)]);
    result = null;
    solvedSnapshot = null;
    solvedRequest = null;
    refresh();
  } finally {
    $("run").disabled = false;
  }
}

$("run").addEventListener("click", run);

$("download").addEventListener("click", () => {
  // 仅当结果未过期时允许下载；下载内容与页面展示的是同一次响应。
  if (!canDownload(result, isStale(solvedSnapshot, $("request").value))) return;
  const blob = new Blob([JSON.stringify(result, null, 2)], { type: "application/json" });
  const link = document.createElement("a");
  const url = URL.createObjectURL(blob);
  link.href = url;
  link.download = "time-ranges.json";
  link.click();
  URL.revokeObjectURL(url);
});

refresh();
