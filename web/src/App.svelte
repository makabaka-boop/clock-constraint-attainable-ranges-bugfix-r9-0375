<script>
  import { solve, loadExample } from './lib/api.js';
  import {
    snapshotOf,
    isStale,
    markSolved,
    canDrawTimeline,
    canShowCycle,
  } from './lib/state.js';

  // ---------- 输入 ----------
  let services = [
    { id: 'gw', lo: 0, hi: 0 },
    { id: 'auth', lo: -50, hi: 50 },
  ];
  let spans = [
    { id: 's1', service: 'gw', parent: '', start: 100, end: 900 },
    { id: 's2', service: 'auth', parent: 's1', start: 150, end: 300 },
  ];

  // ---------- 求解结果 ----------
  let result = null;          // 最近一次求解响应（原样保存）
  let solvedSnapshot = null;  // 求解成功时的输入快照
  let errors = [];
  let loading = false;

  // 输入一旦变化（偏移界或 span），快照失配，旧结果立即失效。
  $: stale = isStale(solvedSnapshot, services, spans);
  $: drawTimeline = canDrawTimeline(result, stale);
  $: showCycle = canShowCycle(result, stale);
  $: showOk = !stale && result !== null && result.status === 'ok';
  $: timeline = drawTimeline ? buildTimeline(result) : null;

  async function doSolve() {
    loading = true;
    errors = [];
    try {
      const r = await solve({ services, spans });
      result = r;
      solvedSnapshot = markSolved(services, spans);
    } catch (e) {
      result = null;
      solvedSnapshot = null;
      errors = e.details || [String(e.message || e)];
    } finally {
      loading = false;
    }
  }

  async function load(kind) {
    errors = [];
    try {
      const ex = await loadExample(kind);
      services = ex.services;
      spans = ex.spans;
    } catch (e) {
      errors = [String(e.message || e)];
    }
  }

  // ---------- 编辑操作 ----------
  function addService() {
    const id = `svc${services.length + 1}`;
    services = [...services, { id, lo: -10, hi: 10 }];
  }
  function removeService(i) {
    const gone = services[i].id;
    services = services.filter((_, j) => j !== i);
    // 清理悬空引用，保持输入自洽。
    spans = spans
      .filter((sp) => sp.service !== gone)
      .map((sp) => sp);
  }
  function addSpan() {
    const id = `s${spans.length + 1}`;
    const svc = services.find((s) => s.lo !== 0 || s.hi !== 0) || services[0];
    spans = [...spans, { id, service: svc.id, parent: '', start: 0, end: 10 }];
  }
  function removeSpan(i) {
    const gone = spans[i].id;
    spans = spans
      .filter((_, j) => j !== i)
      .map((sp) => (sp.parent === gone ? { ...sp, parent: '' } : sp));
  }

  // ---------- 时间线几何 ----------
  const LABEL_W = 110;
  const ROW_PAD = 8;
  const LANE_H = 20;
  const WIDTH = 760;

  function hueOf(id) {
    let h = 0;
    for (const ch of id) h = (h * 31 + ch.charCodeAt(0)) % 360;
    return h;
  }

  function buildTimeline(r) {
    const minT = Math.min(...r.spans.map((s) => s.correctedStart));
    const maxT = Math.max(...r.spans.map((s) => s.correctedEnd));
    const range = Math.max(1, maxT - minT);
    const x = (t) => LABEL_W + ((t - minT) / range) * (WIDTH - LABEL_W - 12);

    // 每个服务一行；行内重叠的 span 分配到不同泳道。
    const rows = [];
    const bars = [];
    let y = ROW_PAD;
    for (const svc of r.order) {
      const mine = r.spans
        .filter((s) => s.service === svc)
        .sort((a, b) => a.correctedStart - b.correctedStart || a.correctedEnd - b.correctedEnd);
      const laneEnd = [];
      for (const sp of mine) {
        let lane = laneEnd.findIndex((end) => end <= sp.correctedStart);
        if (lane === -1) {
          lane = laneEnd.length;
          laneEnd.push(-Infinity);
        }
        laneEnd[lane] = sp.correctedEnd;
        bars.push({
          sp,
          x0: x(sp.correctedStart),
          x1: Math.max(x(sp.correctedEnd), x(sp.correctedStart) + 2),
          y: y + lane * LANE_H,
          hue: hueOf(svc),
        });
      }
      const lanes = Math.max(1, laneEnd.length);
      rows.push({ id: svc, y, height: lanes * LANE_H, offset: r.offsets[svc] });
      y += lanes * LANE_H + ROW_PAD;
    }
    // 父子连线（子条左缘中点 → 父条左缘中点）。
    const byId = new Map(bars.map((b) => [b.sp.id, b]));
    const links = bars
      .filter((b) => b.sp.parent && byId.has(b.sp.parent))
      .map((b) => {
        const p = byId.get(b.sp.parent);
        return { x1: b.x0, y1: b.y + LANE_H / 2 - 2, x2: p.x0, y2: p.y + LANE_H / 2 - 2 };
      });
    return { rows, bars, links, height: y + 14, minT, maxT };
  }

  function edgeText(e) {
    const ref = e.ref || {};
    switch (ref.kind) {
      case 'bound_upper':
        return `服务 ${ref.service} 的偏移上界（x ≤ hi）`;
      case 'bound_lower':
        return `服务 ${ref.service} 的偏移下界（x ≥ lo）`;
      case 'child_start':
        return `子 span ${ref.span} 起点须不早于父 span ${ref.parentSpan} 起点`;
      case 'child_end':
        return `子 span ${ref.span} 终点须不晚于父 span ${ref.parentSpan} 终点`;
      default:
        return '未知约束';
    }
  }

  const nodeName = (n) => (n === '@zero' ? '零点基准' : n);
</script>

<main>
  <h1>时钟偏移校正复核</h1>
  <p class="hint">
    模拟同步调用链：每个服务一个整数时钟偏移区间（恰一个锚定为 [0,0]）。
    校正后每个子调用必须完整落在父调用区间内；后端化为差分约束，
    求按服务 ID 字典序最小的可行偏移向量，无解时返回真实约束构成的矛盾环。
  </p>

  <section class="card">
    <h2>服务（{services.length}/8）</h2>
    <table>
      <thead>
        <tr><th>服务 ID</th><th>偏移下界</th><th>偏移上界</th><th></th><th></th></tr>
      </thead>
      <tbody>
        {#each services as s, i}
          <tr>
            <td><input class="id" bind:value={s.id} /></td>
            <td><input type="number" bind:value={s.lo} /></td>
            <td><input type="number" bind:value={s.hi} /></td>
            <td>{#if s.lo === 0 && s.hi === 0}<span class="anchor">锚点</span>{/if}</td>
            <td><button on:click={() => removeService(i)} disabled={services.length <= 2}>删除</button></td>
          </tr>
        {/each}
      </tbody>
    </table>
    <button on:click={addService} disabled={services.length >= 8}>添加服务</button>
  </section>

  <section class="card">
    <h2>Span（{spans.length}/100）</h2>
    <table>
      <thead>
        <tr><th>ID</th><th>服务</th><th>父 span</th><th>本地开始</th><th>本地结束</th><th></th></tr>
      </thead>
      <tbody>
        {#each spans as sp, i}
          <tr>
            <td><input class="id" bind:value={sp.id} /></td>
            <td>
              <select bind:value={sp.service}>
                {#each services as s}<option value={s.id}>{s.id}</option>{/each}
              </select>
            </td>
            <td>
              <select bind:value={sp.parent}>
                <option value="">（根）</option>
                {#each spans.filter((o) => o.id !== sp.id) as o}
                  <option value={o.id}>{o.id}</option>
                {/each}
              </select>
            </td>
            <td><input type="number" bind:value={sp.start} /></td>
            <td><input type="number" bind:value={sp.end} /></td>
            <td><button on:click={() => removeSpan(i)}>删除</button></td>
          </tr>
        {/each}
      </tbody>
    </table>
    <button on:click={addSpan} disabled={spans.length >= 100}>添加 span</button>
  </section>

  <section class="controls">
    <button class="primary" on:click={doSolve} disabled={loading}>
      {loading ? '求解中…' : '求解'}
    </button>
    <button on:click={() => load('feasible')}>载入可行示例</button>
    <button on:click={() => load('infeasible')}>载入矛盾示例</button>
  </section>

  {#if errors.length > 0}
    <section class="card error">
      <h2>输入未通过校验</h2>
      <ul>{#each errors as e}<li>{e}</li>{/each}</ul>
    </section>
  {/if}

  {#if stale}
    <section class="card stale">
      输入已变更，旧求解结果已失效，请重新求解。
    </section>
  {/if}

  {#if showOk}
    <section class="card">
      <h2>字典序最小可行偏移（按服务 ID）</h2>
      <div class="offsets">
        {#each result.order as id}
          <span class="chip" class:anchored={result.offsets[id] === 0 && services.some((s) => s.id === id && s.lo === 0 && s.hi === 0)}>
            {id}: {result.offsets[id] >= 0 ? '+' : ''}{result.offsets[id]}
          </span>
        {/each}
      </div>
    </section>

    {#if timeline}
      <section class="card">
        <h2>校正时间线（数据来自本次求解响应）</h2>
        <svg width={WIDTH} height={timeline.height} role="img" aria-label="校正时间线">
          {#each timeline.rows as row}
            <text x="4" y={row.y + 14} class="rowlabel">{row.id}</text>
            <text x="4" y={row.y + 28} class="rowsub">偏移 {row.offset >= 0 ? '+' : ''}{row.offset}</text>
            <line x1={LABEL_W} y1={row.y + row.height / 2} x2={WIDTH - 8} y2={row.y + row.height / 2} class="rowline" />
          {/each}
          {#each timeline.links as l}
            <line x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} class="link" />
          {/each}
          {#each timeline.bars as b}
            <rect x={b.x0} y={b.y + 2} width={b.x1 - b.x0} height={14}
                  rx="3" fill={`hsl(${b.hue} 65% 55% / 0.85)`}>
              <title>{b.sp.id} @ {b.sp.service}：校正 [{b.sp.correctedStart}, {b.sp.correctedEnd}]（本地 [{b.sp.start}, {b.sp.end}]）</title>
            </rect>
            <text x={b.x0 + 3} y={b.y + 13} class="barlabel">{b.sp.id}</text>
          {/each}
          <text x={LABEL_W} y={timeline.height - 2} class="axis">{timeline.minT}</text>
          <text x={WIDTH - 30} y={timeline.height - 2} class="axis">{timeline.maxT}</text>
        </svg>
      </section>
    {/if}

    {#if result.constraints.length > 0}
      <section class="card">
        <h2>父子约束余量</h2>
        <table>
          <thead><tr><th>子 span</th><th>父 span</th><th>起点余量</th><th>终点余量</th></tr></thead>
          <tbody>
            {#each result.constraints as c}
              <tr>
                <td>{c.span}</td><td>{c.parentSpan}</td>
                <td class:tight={c.startSlack === 0}>{c.startSlack}</td>
                <td class:tight={c.endSlack === 0}>{c.endSlack}</td>
              </tr>
            {/each}
          </tbody>
        </table>
        <p class="hint">余量为 0 表示该侧已顶到父调用边界（紧约束）。</p>
      </section>
    {/if}
  {/if}

  {#if showCycle}
    <section class="card cycle">
      <h2>不可行：矛盾环（总权重 {result.cycle.totalWeight}）</h2>
      <p class="hint">以下每条边都对应一条真实输入约束，首尾相接且权重和为负，故无可行校正。不绘制校正时间线。</p>
      <ol>
        {#each result.cycle.edges as e}
          <li>
            <code>{nodeName(e.from)} → {nodeName(e.to)}</code>
            <span class="w">权重 {e.weight}</span> — {edgeText(e)}
          </li>
        {/each}
      </ol>
    </section>
  {/if}
</main>

<style>
  :global(body) {
    margin: 0;
    background: #f4f5f7;
    color: #1f2430;
    font-family: system-ui, -apple-system, 'Segoe UI', sans-serif;
  }
  main { max-width: 860px; margin: 0 auto; padding: 24px 16px 64px; }
  h1 { font-size: 22px; margin: 0 0 8px; }
  h2 { font-size: 15px; margin: 0 0 10px; }
  .hint { color: #667085; font-size: 13px; margin: 4px 0 0; }
  .card {
    background: #fff; border: 1px solid #e4e7ec; border-radius: 10px;
    padding: 14px 16px; margin-top: 16px;
  }
  .card.error { border-color: #f04438; }
  .card.error li { color: #b42318; font-size: 13px; }
  .card.stale { border-color: #f79009; color: #93370d; font-size: 14px; }
  .card.cycle { border-color: #f04438; }
  .card.cycle li { font-size: 13px; margin: 4px 0; }
  .card.cycle .w { color: #b42318; font-weight: 600; }
  table { border-collapse: collapse; width: 100%; margin-bottom: 8px; }
  th, td { text-align: left; padding: 4px 6px; font-size: 13px; }
  th { color: #667085; font-weight: 600; }
  input, select {
    font: inherit; padding: 3px 6px; border: 1px solid #d0d5dd;
    border-radius: 6px; width: 90px;
  }
  input.id { width: 80px; }
  button {
    font: inherit; padding: 4px 12px; border: 1px solid #d0d5dd;
    border-radius: 6px; background: #fff; cursor: pointer;
  }
  button:disabled { opacity: 0.45; cursor: default; }
  button.primary { background: #155eef; border-color: #155eef; color: #fff; }
  .controls { margin-top: 16px; display: flex; gap: 8px; }
  .anchor {
    background: #e0eaff; color: #155eef; border-radius: 999px;
    font-size: 12px; padding: 1px 8px;
  }
  .offsets { display: flex; gap: 8px; flex-wrap: wrap; }
  .chip {
    background: #eef2f6; border-radius: 999px; padding: 3px 12px;
    font-family: ui-monospace, monospace; font-size: 13px;
  }
  .chip.anchored { background: #e0eaff; color: #155eef; }
  .tight { color: #b42318; font-weight: 700; }
  .rowlabel { font-size: 12px; fill: #1f2430; font-weight: 600; }
  .rowsub { font-size: 10px; fill: #667085; }
  .rowline { stroke: #e4e7ec; stroke-width: 1; }
  .link { stroke: #98a2b3; stroke-width: 1; stroke-dasharray: 3 2; opacity: 0.7; }
  .barlabel { font-size: 9px; fill: #fff; pointer-events: none; }
  .axis { font-size: 10px; fill: #667085; }
  code { font-family: ui-monospace, monospace; font-size: 12px; }
</style>
