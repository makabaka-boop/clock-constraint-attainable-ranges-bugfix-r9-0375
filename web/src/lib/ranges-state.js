// /ranges 复核页的状态纯函数：编辑输入后旧范围与“下载同次证据”立即失效。
// 与主求解页的 state.js 同构，但快照额外包含查询列表，且下载门禁要求
// 当前结果确实是一次可行（ok）复核——无解时只有矛盾环，没有可下载的范围证据。

export function rangesSnapshot(requestText) {
  return JSON.stringify({ request: requestText });
}

export function isRangesStale(solvedSnapshot, requestText) {
  return (
    solvedSnapshot !== null &&
    rangesSnapshot(requestText) !== solvedSnapshot
  );
}

// 复核成功（无论可行还是无解）时记录当时请求文本的快照。
export function markRangesSolved(requestText) {
  return rangesSnapshot(requestText);
}

export function visibleRangesResult(result, stale) {
  return stale ? null : result;
}

// 只有“未过期的结果”才展示面板。
export function canShowRanges(result, stale) {
  return visibleRangesResult(result, stale) !== null;
}

// 只有“未过期且原输入可行”的结果才允许下载同次范围证据；
// 编辑后失效、或无解（仅矛盾环）时按钮禁用。
export function canDownloadRanges(result, stale) {
  const r = visibleRangesResult(result, stale);
  return r !== null && r.base != null && r.base.status === 'ok';
}

// 无解但仍有效（未被编辑失效）时，只展示矛盾环。
export function canShowRangesCycle(result, stale) {
  const r = visibleRangesResult(result, stale);
  return r !== null && r.base != null && r.base.status === 'infeasible' &&
    Array.isArray(r.ranges) === false;
}
