package solver

import (
	"fmt"
	"strings"
)

// MaxQueries 是单次范围复核允许的查询数量上限。
const MaxQueries = 16

// TimePoint 引用某条 span 的起点或终点（该服务本地时钟下的时刻）。
type TimePoint struct {
	Span  string `json:"span"`
	Point string `json:"point"`
}

// RangeQuery 比较两个时刻：校正后 right 减 left 的差值能落到什么范围。
type RangeQuery struct {
	ID    string    `json:"id"`
	Left  TimePoint `json:"left"`
	Right TimePoint `json:"right"`
}

// RangesInput 是范围复核请求：原调用链输入加一组时刻差查询。
type RangesInput struct {
	Input   Input        `json:"input"`
	Queries []RangeQuery `json:"queries"`
}

// TimeRange 是一个查询的结果。Min/Max 是校正后差值在全部约束（所有服务
// 偏移界 + 所有父子调用关系）下的全局最小/最大值；MinWitness/MaxWitness
// 分别是确实达到该端点的完整偏移见证：覆盖全部服务、落在各自偏移界内、
// 满足每一条父子约束。两个端点不要求来自同一份见证。
type TimeRange struct {
	ID         string         `json:"id"`
	Left       TimePoint      `json:"left"`
	Right      TimePoint      `json:"right"`
	Min        int64          `json:"min"`
	Max        int64          `json:"max"`
	MinWitness map[string]int `json:"minWitness"`
	MaxWitness map[string]int `json:"maxWitness"`
}

// RangesResult 的 Base 是原偏移求解结果；原输入无解时只有 Base（含矛盾
// 环证据），不携带任何范围。
type RangesResult struct {
	Base   Result      `json:"base"`
	Ranges []TimeRange `json:"ranges,omitempty"`
}

// RequestError 表示请求未通过校验，应整份拒绝（对应 HTTP 400）。
type RequestError struct {
	Problems []string
}

func (e *RequestError) Error() string {
	return "请求非法: " + strings.Join(e.Problems, "；")
}

// SolveRanges 计算每个查询的校正时刻差在全部约束下的全局最小/最大值，
// 两端分别附达到该值的完整偏移见证。
//
// 记左/右时刻所在服务节点为 a、b，本地时刻为 l、r，则校正差值
// D = (r + x[b]) - (l + x[a]) = (r-l) + (x[b]-x[a])。差分约束系统里
// x[b]-x[a] <= dist(a,b)（最短路即最紧上界），且该界可达：向图中加入
// 边 b->a（权 -dist(a,b)）不产生负环，其零点最短路向量就是达到等号的
// 可行偏移。最小值对称可得。同一服务的两个时刻共享同一偏移，差值退化为
// 常数 r-l（min == max）；负差值与相等时刻都允许。结果的 ID 与顺序与
// 请求中的查询一一对应。
func SolveRanges(request RangesInput) (RangesResult, error) {
	if errs := Validate(request.Input); len(errs) > 0 {
		return RangesResult{}, &RequestError{Problems: errs}
	}
	if errs := validateQueries(request.Input, request.Queries); len(errs) > 0 {
		return RangesResult{}, &RequestError{Problems: errs}
	}
	base, err := Solve(request.Input)
	if err != nil {
		return RangesResult{}, err
	}
	out := RangesResult{Base: base}
	if base.Status != "ok" {
		// 原输入无解：只交付矛盾环证据，不给出任何范围。
		return out, nil
	}

	n, idx, order, edges := buildGraph(request.Input)
	dist := floydWarshall(n, edges)
	spans := make(map[string]Span, len(request.Input.Spans))
	for _, sp := range request.Input.Spans {
		spans[sp.ID] = sp
	}
	for _, q := range request.Queries {
		ls, rs := spans[q.Left.Span], spans[q.Right.Span]
		l, r := pointTime(ls, q.Left.Point), pointTime(rs, q.Right.Point)
		a, b := idx[ls.Service], idx[rs.Service]
		tr := TimeRange{ID: q.ID, Left: q.Left, Right: q.Right}
		tr.Max = int64(r-l) + dist[a][b]
		tr.Min = int64(r-l) - dist[b][a]
		tr.MaxWitness = diffWitness(idx, order, dist, a, b)
		if err := checkWitness(edges, order, tr.MaxWitness, a, b, dist[a][b]); err != nil {
			return RangesResult{}, err
		}
		tr.MinWitness = diffWitness(idx, order, dist, b, a)
		if err := checkWitness(edges, order, tr.MinWitness, b, a, dist[b][a]); err != nil {
			return RangesResult{}, err
		}
		out.Ranges = append(out.Ranges, tr)
	}
	return out, nil
}

// pointTime 取时刻点引用的本地时刻；point 已在校验阶段限制为 start/end。
func pointTime(sp Span, point string) int {
	if point == "start" {
		return sp.Start
	}
	return sp.End
}

// validateQueries 校验查询列表：数量上限、ID 非空且唯一、时刻引用合法。
// 任一问题都导致整份拒绝。
func validateQueries(in Input, queries []RangeQuery) []string {
	var errs []string
	if len(queries) > MaxQueries {
		errs = append(errs, fmt.Sprintf("查询数量至多为 %d，当前 %d", MaxQueries, len(queries)))
	}
	spans := make(map[string]Span, len(in.Spans))
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	seen := make(map[string]bool, len(queries))
	checkPoint := func(qid, side string, p TimePoint) {
		if _, ok := spans[p.Span]; !ok {
			errs = append(errs, fmt.Sprintf("查询 %q 的%s引用了不存在的 span %q", qid, side, p.Span))
		}
		if p.Point != "start" && p.Point != "end" {
			errs = append(errs, fmt.Sprintf("查询 %q 的%s时刻类型 %q 非法，应为 start 或 end", qid, side, p.Point))
		}
	}
	for i, q := range queries {
		if q.ID == "" {
			errs = append(errs, fmt.Sprintf("第 %d 个查询的 ID 不能为空", i+1))
		} else if seen[q.ID] {
			errs = append(errs, fmt.Sprintf("查询 ID %q 重复", q.ID))
		}
		seen[q.ID] = true
		checkPoint(q.ID, "左侧", q.Left)
		checkPoint(q.ID, "右侧", q.Right)
	}
	return errs
}

// diffWitness 构造达到 x[b]-x[a] == dist(a,b) 的可行偏移向量：等价于在
// 图中加入边 b->a（权 -dist(a,b)）后从零点出发的最短路
// x_i = min(dist(0,i), dist(0,b) - dist(a,b) + dist(a,i))。
// 新边只会出现在 0~>b->a~>i 形路径上，且环 b->a~>b 权重为 0，故不产生
// 负环，该向量满足全部原约束（含各服务偏移界）。
func diffWitness(idx map[string]int, order []string, dist [][]int64, a, b int) map[string]int {
	w := make(map[string]int, len(order))
	for _, id := range order {
		i := idx[id]
		v := dist[0][i]
		if alt := dist[0][b] - dist[a][b] + dist[a][i]; alt < v {
			v = alt
		}
		w[id] = int(v)
	}
	return w
}

// checkWitness 防御性复核：见证必须满足全部约束，且 x[b]-x[a] 确实等于
// 声明的端点值。
func checkWitness(edges []edge, order []string, w map[string]int, a, b int, want int64) error {
	at := func(node int) int64 {
		if node == 0 {
			return 0
		}
		return int64(w[order[node-1]])
	}
	for _, e := range edges {
		if at(e.to)-at(e.from) > int64(e.w) {
			return fmt.Errorf("内部错误：范围见证违反约束 %s", e.ref.Kind)
		}
	}
	if got := at(b) - at(a); got != want {
		return fmt.Errorf("内部错误：范围见证差值 %d 未达到声明端点 %d", got, want)
	}
	return nil
}
