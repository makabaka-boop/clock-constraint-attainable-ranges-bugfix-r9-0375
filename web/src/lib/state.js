// 结果失效（stale）判定与结果可见性的纯函数，供组件与测试共用。
//
// 核心约定：求解成功后记录当时输入的快照；此后输入（偏移界或 span）
// 有任何变化，快照不再匹配，旧结果立即失效——时间线与余量都不再展示，
// 更不会在 infeasible 时绘制伪造的校正时间线。

export function snapshotOf(services, spans) {
  return JSON.stringify({ services, spans });
}

export function isStale(solvedSnapshot, services, spans) {
  return (
    solvedSnapshot !== null && snapshotOf(services, spans) !== solvedSnapshot
  );
}

// 求解成功时调用：返回应记录的快照。
export function markSolved(services, spans) {
  return snapshotOf(services, spans);
}

export function visibleResult(result, stale) {
  return stale ? null : result;
}

// 只有“未过期的 ok 结果”才允许绘制校正时间线。
export function canDrawTimeline(result, stale) {
  const r = visibleResult(result, stale);
  return (
    r !== null &&
    r.status === "ok" &&
    Array.isArray(r.spans) &&
    r.spans.length > 0
  );
}

// 只有“未过期的 infeasible 结果”才展示矛盾环。
export function canShowCycle(result, stale) {
  const r = visibleResult(result, stale);
  return r !== null && r.status === "infeasible" && r.cycle != null;
}
