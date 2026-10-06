package solver

import "fmt"

// MaxQueries 限制一次请求中的查询数量。
const MaxQueries = 16

// TimePoint 引用某个 span 的本地 start 或 end 时刻。
type TimePoint struct {
	Span  string `json:"span"`
	Point string `json:"point"` // "start" 或 "end"
}

// RangeQuery 比较两个校正后时刻：右时刻 - 左时刻。
type RangeQuery struct {
	ID    string    `json:"id"`
	Left  TimePoint `json:"left"`
	Right TimePoint `json:"right"`
}

// RangesInput 是 /api/ranges 的请求：与 /api/solve 相同的输入加一组查询。
type RangesInput struct {
	Input   Input        `json:"input"`
	Queries []RangeQuery `json:"queries"`
}

// TimeRange 是单个查询的可行范围。MinWitness/MaxWitness 各自是一份
// 完整、可行（满足全部偏移界与父子约束）且确实达到对应端点的偏移向量；
// 两份见证不要求相同。Left/Right 原样保留查询的时刻引用，使证据自包含。
type TimeRange struct {
	ID         string         `json:"id"`
	Left       TimePoint      `json:"left"`
	Right      TimePoint      `json:"right"`
	Min        int64          `json:"min"`
	Max        int64          `json:"max"`
	MinWitness map[string]int `json:"minWitness"`
	MaxWitness map[string]int `json:"maxWitness"`
}

// RangesResult 与 Solve 的结果互斥分两种：
// 可行时携带字典序基准解与逐查询范围；无解时只携带 Base 中的矛盾环，
// Ranges 为 nil，不含任何可用于绘制时间线的字段。
type RangesResult struct {
	Base   Result      `json:"base"`
	Ranges []TimeRange `json:"ranges,omitempty"`
}

// pointValue 解析时刻引用，返回 span、对应服务节点与本地时刻值。
func pointValue(spans map[string]Span, idx map[string]int, p TimePoint) (node, val int, sp Span, err error) {
	s, ok := spans[p.Span]
	if !ok {
		return 0, 0, Span{}, fmt.Errorf("查询引用了未知 span %q", p.Span)
	}
	switch p.Point {
	case "start":
		return idx[s.Service], s.Start, s, nil
	case "end":
		return idx[s.Service], s.End, s, nil
	default:
		return 0, 0, Span{}, fmt.Errorf("查询 %q 的时刻类型 %q 非法（仅支持 start/end）", p.Span, p.Point)
	}
}

// ValidateRangesRequest 校验整份请求：输入本身合法、查询数量与 ID 合法、
// 每个时刻引用合法。任一项不合法都应整份拒绝。
func ValidateRangesRequest(req RangesInput) []string {
	errs := Validate(req.Input)
	if len(req.Queries) == 0 {
		errs = append(errs, "至少需要一个查询")
	} else if len(req.Queries) > MaxQueries {
		errs = append(errs, fmt.Sprintf("查询数量至多为 %d，当前 %d", MaxQueries, len(req.Queries)))
	}
	spans := make(map[string]Span, len(req.Input.Spans))
	for _, sp := range req.Input.Spans {
		spans[sp.ID] = sp
	}
	seenID := map[string]bool{}
	for i, q := range req.Queries {
		where := fmt.Sprintf("第 %d 个查询", i+1)
		if q.ID == "" {
			errs = append(errs, where+"缺少 ID")
		} else if seenID[q.ID] {
			errs = append(errs, fmt.Sprintf("%s的 ID %q 重复", where, q.ID))
		}
		seenID[q.ID] = true
		for side, tp := range map[string]TimePoint{"左端": q.Left, "右端": q.Right} {
			sp, ok := spans[tp.Span]
			if tp.Span == "" || !ok {
				errs = append(errs, fmt.Sprintf("%s（ID %q）%s引用了未知 span %q", where, q.ID, side, tp.Span))
				continue
			}
			if tp.Point != "start" && tp.Point != "end" {
				errs = append(errs, fmt.Sprintf("%s（ID %q）%s引用 span %s 的时刻类型 %q 非法（仅支持 start/end）",
					where, q.ID, side, sp.ID, tp.Point))
			}
		}
	}
	return errs
}

// SolveRanges 在与 /api/solve 完全相同的差分约束系统上，逐查询求
// 校正后（右时刻 - 左时刻）的全局最小/最大值。
//
// 设左、右时刻分别位于服务 a、b，本地值为 l、r，目标量
// D = (r + x[b]) - (l + x[a]) = (r-l) + x[b]-x[a]。
// 在“全部边 x[v]-x[u] <= w”的可行系统中（无负环）：
//
//	max (x[b]-x[a]) = dist(a,b)，  min (x[b]-x[a]) = -dist(b,a)，
//
//	其中 dist 为全源最短路（节点 0 是固定为 0 的零点）。
//
// 端点可达性（见证构造）：给定源 s，赋值 x[v] = dist(s,v) - dist(s,0)
// 满足 x[0]=0 且对每条边 u->v 有 x[v]-x[u] <= w（最短路三角不等式），
// 因而是一份完整可行偏移。取 s=a 时 x[b]-x[a]=dist(a,b)（达最大）；
// 取 s=b 时 x[b]-x[a]=-dist(b,a)（达最小）。同一服务 a==b 时
// dist(a,a)=0，两端点相等，偏移变化不影响同服务时刻差。
func SolveRanges(request RangesInput) (RangesResult, error) {
	base, err := Solve(request.Input)
	if err != nil {
		return RangesResult{}, err
	}
	out := RangesResult{Base: base}
	if base.Status != "ok" {
		// 无解：只交付矛盾环，不计算任何范围。
		return out, nil
	}

	n, idx, order, edges := buildGraph(request.Input)
	dist := floydWarshall(n, edges)
	spans := make(map[string]Span, len(request.Input.Spans))
	for _, sp := range request.Input.Spans {
		spans[sp.ID] = sp
	}
	nodeID := func(node int) string { return order[node-1] }

	for _, q := range request.Queries {
		a, l, _, err := pointValue(spans, idx, q.Left)
		if err != nil {
			return RangesResult{}, err
		}
		b, r, _, err := pointValue(spans, idx, q.Right)
		if err != nil {
			return RangesResult{}, err
		}
		if dist[a][b] >= inf/2 || dist[b][a] >= inf/2 {
			return RangesResult{}, fmt.Errorf("内部错误：服务间约束不可达") // 理论不可达：界边使图强连通
		}
		constDelta := int64(r - l)
		tr := TimeRange{
			ID:    q.ID,
			Left:  q.Left,
			Right: q.Right,
			Min:   constDelta - dist[b][a],
			Max:   constDelta + dist[a][b],
		}
		if tr.Min > tr.Max {
			return RangesResult{}, fmt.Errorf("内部错误：查询 %q 计算出的最小端 %d 大于最大端 %d", q.ID, tr.Min, tr.Max)
		}
		// s=a 达到最大端，s=b 达到最小端（见函数注释）。
		maxW, err := witnessFrom(dist, edges, idx, order, a)
		if err != nil {
			return RangesResult{}, err
		}
		minW, err := witnessFrom(dist, edges, idx, order, b)
		if err != nil {
			return RangesResult{}, err
		}
		// 逐份见证独立复核：必须可行，且目标量确实等于所声明端点。
		deltaIn := func(w map[string]int) int64 {
			return int64(w[nodeID(b)] - w[nodeID(a)])
		}
		if got := constDelta + deltaIn(maxW); got != tr.Max {
			return RangesResult{}, fmt.Errorf("内部错误：查询 %q 最大见证只达到 %d，声明 %d", q.ID, got, tr.Max)
		}
		if got := constDelta + deltaIn(minW); got != tr.Min {
			return RangesResult{}, fmt.Errorf("内部错误：查询 %q 最小见证只达到 %d，声明 %d", q.ID, got, tr.Min)
		}
		tr.MaxWitness, tr.MinWitness = maxW, minW
		out.Ranges = append(out.Ranges, tr)
	}
	return out, nil
}

// witnessFrom 按 x[v] = dist(src,v) - dist(src,0) 构造一份完整可行偏移，
// 并防御性复核全部约束边。
func witnessFrom(dist [][]int64, edges []edge, idx map[string]int, order []string, src int) (map[string]int, error) {
	n := len(dist)
	if dist[src][0] >= inf/2 {
		return nil, fmt.Errorf("内部错误：节点 %d 到零点无路径", src)
	}
	x := make([]int64, n)
	for v := 0; v < n; v++ {
		if dist[src][v] >= inf/2 {
			return nil, fmt.Errorf("内部错误：节点 %d 到节点 %d 无路径", src, v)
		}
		x[v] = dist[src][v] - dist[src][0]
	}
	if x[0] != 0 {
		return nil, fmt.Errorf("内部错误：见证的零点偏移为 %d", x[0])
	}
	for _, e := range edges {
		if x[e.to]-x[e.from] > int64(e.w) {
			return nil, fmt.Errorf("内部错误：见证违反约束边 %s->%s（w=%d）",
				nodeLabel(e.from, order), nodeLabel(e.to, order), e.w)
		}
	}
	w := make(map[string]int, len(order))
	for _, id := range order {
		w[id] = int(x[idx[id]])
	}
	return w, nil
}
