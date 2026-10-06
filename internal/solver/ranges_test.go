package solver

import (
	"fmt"
	"math/rand"
	"testing"
)

// ---------- 穷举对拍 ----------

// enumerateOffsets 枚举界内全部整数偏移向量。
func enumerateOffsets(in Input) [][]int {
	_, _, order, _ := buildGraph(in)
	svc := map[string]Service{}
	for _, s := range in.Services {
		svc[s.ID] = s
	}
	var out [][]int
	cur := make([]int, len(order))
	var rec func(i int)
	rec = func(i int) {
		if i == len(order) {
			out = append(out, append([]int(nil), cur...))
			return
		}
		s := svc[order[i]]
		for v := s.Lo; v <= s.Hi; v++ {
			cur[i] = v
			rec(i + 1)
		}
	}
	rec(0)
	return out
}

// witnessFeasible 独立核验见证：偏移在界内、锚点为 0、满足全部父子约束。
func witnessFeasible(t *testing.T, in Input, order []string, w map[string]int) {
	t.Helper()
	svc := map[string]Service{}
	for _, s := range in.Services {
		svc[s.ID] = s
		if v, ok := w[s.ID]; !ok {
			t.Fatalf("见证缺少服务 %s 的偏移", s.ID)
		} else if v < s.Lo || v > s.Hi {
			t.Fatalf("见证中服务 %s 偏移 %d 越界 [%d,%d]", s.ID, v, s.Lo, s.Hi)
		}
		if s.Lo == 0 && s.Hi == 0 && w[s.ID] != 0 {
			t.Fatalf("见证破坏锚点服务 %s（偏移 %d）", s.ID, w[s.ID])
		}
	}
	for id := range w {
		if _, ok := svc[id]; !ok {
			t.Fatalf("见证出现未知服务 %q", id)
		}
	}
	if !feasibleVec(in, order, offsetsByOrder(order, w)) {
		t.Fatalf("见证不满足全部父子约束: %+v", w)
	}
}

func offsetsByOrder(order []string, w map[string]int) []int {
	v := make([]int, len(order))
	for i, id := range order {
		v[i] = w[id]
	}
	return v
}

func pointLocal(sp Span, point string) int {
	if point == "start" {
		return sp.Start
	}
	return sp.End
}

func queryValue(in Input, order []string, vec []int, q RangeQuery) int64 {
	svc := map[string]int{}
	for i, id := range order {
		svc[id] = vec[i]
	}
	spans := map[string]Span{}
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	l, r := spans[q.Left.Span], spans[q.Right.Span]
	return int64(pointLocal(r, q.Right.Point)-pointLocal(l, q.Left.Point)) +
		int64(svc[r.Service]-svc[l.Service])
}

// ---------- 对拍：穷举全部偏移向量与全部查询 ----------

func randRangesRequest(r *rand.Rand, in Input) RangesInput {
	req := RangesInput{Input: in}
	if len(in.Spans) == 0 {
		return req
	}
	n := 1 + r.Intn(4)
	for i := 0; i < n; i++ {
		l := in.Spans[r.Intn(len(in.Spans))]
		rr := in.Spans[r.Intn(len(in.Spans))]
		pick := func() string {
			if r.Intn(2) == 0 {
				return "start"
			}
			return "end"
		}
		req.Queries = append(req.Queries, RangeQuery{
			ID:    fmt.Sprintf("q%d", i),
			Left:  TimePoint{Span: l.ID, Point: pick()},
			Right: TimePoint{Span: rr.ID, Point: pick()},
		})
	}
	return req
}

func TestRangesAgainstBruteForce(t *testing.T) {
	const iterations = 3000
	for seed := int64(0); seed < iterations; seed++ {
		r := rand.New(rand.NewSource(seed))
		in := randInput(r)
		req := randRangesRequest(r, in)
		if errs := ValidateRangesRequest(req); len(errs) != 0 {
			t.Fatalf("seed %d: 生成器产生了非法请求: %v", seed, errs)
		}
		res, err := SolveRanges(req)
		if err != nil {
			t.Fatalf("seed %d: SolveRanges 错误: %v", seed, err)
		}
		if res.Base.Status != "ok" {
			// 随机生成器理论上总可行；若真无解必须给出真实矛盾环。
			if res.Base.Cycle == nil {
				t.Fatalf("seed %d: 无解但缺少矛盾环", seed)
			}
			checkCycleEvidence(t, in, res.Base.Cycle)
			continue
		}
		_, _, order, _ := buildGraph(in)
		vecs := enumerateOffsets(in)
		var feasible [][]int
		for _, v := range vecs {
			if feasibleVec(in, order, v) {
				feasible = append(feasible, v)
			}
		}
		if len(feasible) == 0 {
			t.Fatalf("seed %d: 基准称可行但穷举无可行向量", seed)
		}
		if len(res.Ranges) != len(req.Queries) {
			t.Fatalf("seed %d: 范围数量 %d != 查询数量 %d（顺序/身份须保留）",
				seed, len(res.Ranges), len(req.Queries))
		}
		for i, q := range req.Queries {
			tr := res.Ranges[i]
			if tr.ID != q.ID {
				t.Fatalf("seed %d: 第 %d 条 ID 从 %q 变成 %q", seed, i, q.ID, tr.ID)
			}
			wantMin, wantMax := queryValue(in, order, feasible[0], q), queryValue(in, order, feasible[0], q)
			for _, v := range feasible {
				got := queryValue(in, order, v, q)
				if got < wantMin {
					wantMin = got
				}
				if got > wantMax {
					wantMax = got
				}
			}
			if tr.Min != wantMin || tr.Max != wantMax {
				t.Fatalf("seed %d 查询 %s: 端点 (%d,%d) 与穷举 (%d,%d) 不符\n输入 %+v",
					seed, q.ID, tr.Min, tr.Max, wantMin, wantMax, in)
			}
			if tr.Min > tr.Max {
				t.Fatalf("seed %d 查询 %s: min>max", seed, q.ID)
			}
			// 两份见证分别复核：可行 + 真正达到端点。
			gotMin := queryValue(in, order, offsetsByOrder(order, tr.MinWitness), q)
			gotMax := queryValue(in, order, offsetsByOrder(order, tr.MaxWitness), q)
			if gotMin != tr.Min {
				t.Fatalf("seed %d 查询 %s: 最小见证值 %d != 声明 %d", seed, q.ID, gotMin, tr.Min)
			}
			if gotMax != tr.Max {
				t.Fatalf("seed %d 查询 %s: 最大见证值 %d != 声明 %d", seed, q.ID, gotMax, tr.Max)
			}
			witnessFeasible(t, in, order, tr.MinWitness)
			witnessFeasible(t, in, order, tr.MaxWitness)
		}
	}
}

// ---------- 定向用例 ----------

// TestRangesParentChildTightensBounds 复现“只看服务界会过宽”的缺陷：
// 父子同时刻约束把 b 相对 a 的偏移差压到 [0,10]，而不是服务界给出的 [-10,10]。
func TestRangesParentChildTightensBounds(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -10, Hi: 10}},
		Spans: []Span{
			{ID: "p", Service: "a", Start: 0, End: 100},
			{ID: "c", Service: "b", Parent: "p", Start: 0, End: 10},
		},
	}
	req := RangesInput{Input: in, Queries: []RangeQuery{{
		ID: "q", Left: TimePoint{Span: "p", Point: "start"}, Right: TimePoint{Span: "c", Point: "start"},
	}}}
	res, err := SolveRanges(req)
	if err != nil || res.Base.Status != "ok" {
		t.Fatalf("期望可行: %+v err=%v", res, err)
	}
	tr := res.Ranges[0]
	// 起点侧要求 x_b>=0；服务界给到 x_b<=10（终点侧余量 90 不限制），故 [0,10]。
	if tr.Min != 0 || tr.Max != 10 {
		t.Fatalf("父子约束下端点应为 [0,10]，得到 [%d,%d]", tr.Min, tr.Max)
	}
	// 最大见证须顶到 x_b=10；最小见证须顶到 x_b=0，且 x_a 恒为 0。
	if tr.MaxWitness["b"] != 10 || tr.MaxWitness["a"] != 0 {
		t.Fatalf("最大见证错误: %+v", tr.MaxWitness)
	}
	if tr.MinWitness["b"] != 0 || tr.MinWitness["a"] != 0 {
		t.Fatalf("最小见证错误: %+v", tr.MinWitness)
	}
}

// TestRangesSameServiceNoPhantomVariation 同一服务两个时刻共享偏移，
// 范围必须恰好是本地时刻差的单点，不允许伪造出“变化”。
func TestRangesSameServiceNoPhantomVariation(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -10, Hi: 10}},
		Spans: []Span{
			{ID: "p", Service: "a", Start: 0, End: 100},
			{ID: "c", Service: "b", Parent: "p", Start: 0, End: 10},
			{ID: "d", Service: "b", Parent: "p", Start: 20, End: 30},
		},
	}
	req := RangesInput{Input: in, Queries: []RangeQuery{{
		ID:    "same",
		Left:  TimePoint{Span: "c", Point: "start"},
		Right: TimePoint{Span: "d", Point: "end"},
	}}}
	res, _ := SolveRanges(req)
	tr := res.Ranges[0]
	// 右=30 左=0，同服务偏移抵消：恒为 30，min==max。
	if tr.Min != 30 || tr.Max != 30 {
		t.Fatalf("同服务时刻差应为单点 30，得到 [%d,%d]", tr.Min, tr.Max)
	}
	if queryValue(in, []string{"a", "b"}, offsetsByOrder([]string{"a", "b"}, tr.MinWitness), req.Queries[0]) != 30 ||
		queryValue(in, []string{"a", "b"}, offsetsByOrder([]string{"a", "b"}, tr.MaxWitness), req.Queries[0]) != 30 {
		t.Fatal("同服务见证未达到 30")
	}
	// 同一时刻与自身比较恒为 0（允许相等时刻）。
	req.Queries[0] = RangeQuery{ID: "zero",
		Left: TimePoint{Span: "c", Point: "start"}, Right: TimePoint{Span: "c", Point: "start"}}
	res, _ = SolveRanges(req)
	if res.Ranges[0].Min != 0 || res.Ranges[0].Max != 0 {
		t.Fatalf("自身差应为 0，得到 [%d,%d]", res.Ranges[0].Min, res.Ranges[0].Max)
	}
}

// TestRangesNegativeAndEndConstraints 允许负差值；终点约束独立收紧范围。
func TestRangesNegativeAndEndConstraints(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -10, Hi: 10}},
		Spans: []Span{
			{ID: "p", Service: "a", Start: 0, End: 10},
			{ID: "c", Service: "b", Parent: "p", Start: 5, End: 8},
		},
	}
	// 约束：x_b >= -5（起点侧），x_b <= 2（终点侧）。
	req := RangesInput{Input: in, Queries: []RangeQuery{{
		ID:    "neg",
		Left:  TimePoint{Span: "p", Point: "end"}, // 左=10+x_a
		Right: TimePoint{Span: "c", Point: "end"}, // 右=8+x_b
	}}}
	res, _ := SolveRanges(req)
	tr := res.Ranges[0]
	// D = 8+x_b - 10 = x_b-2 ∈ [-7, 0]，出现负端点。
	if tr.Min != -7 || tr.Max != 0 {
		t.Fatalf("期望 [-7,0]，得到 [%d,%d]", tr.Min, tr.Max)
	}
	if tr.MinWitness["b"] != -5 || tr.MaxWitness["b"] != 2 {
		t.Fatalf("见证偏移错误: min=%+v max=%+v", tr.MinWitness, tr.MaxWitness)
	}
}

// TestRangesKeepsIDAndOrder 查询身份与请求顺序原样保留。
func TestRangesKeepsIDAndOrder(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -2, Hi: 2}},
		Spans:    []Span{{ID: "s1", Service: "a", Start: 0, End: 9}, {ID: "s2", Service: "b", Parent: "s1", Start: 1, End: 8}},
	}
	ids := []string{"z-last", "m-mid", "a-first"}
	req := RangesInput{Input: in}
	for _, id := range ids {
		req.Queries = append(req.Queries, RangeQuery{
			ID: id, Left: TimePoint{Span: "s1", Point: "start"}, Right: TimePoint{Span: "s2", Point: "start"},
		})
	}
	res, err := SolveRanges(req)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if res.Ranges[i].ID != id {
			t.Fatalf("位置 %d 的 ID 应为 %q，实际 %q", i, id, res.Ranges[i].ID)
		}
	}
}

// TestRangesInfeasibleOnlyCycle 输入无解时只交付矛盾环，不出现任何范围。
func TestRangesInfeasibleOnlyCycle(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: 8, Hi: 20}},
		Spans: []Span{
			{ID: "s1", Service: "a", Start: 0, End: 3},
			{ID: "s2", Service: "b", Parent: "s1", Start: 0, End: 10},
		},
	}
	req := RangesInput{Input: in, Queries: []RangeQuery{{
		ID: "q", Left: TimePoint{Span: "s1", Point: "start"}, Right: TimePoint{Span: "s2", Point: "start"},
	}}}
	if errs := ValidateRangesRequest(req); len(errs) != 0 {
		t.Fatalf("请求应合法: %v", errs)
	}
	res, err := SolveRanges(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Base.Status != "infeasible" || res.Base.Cycle == nil {
		t.Fatalf("应只返回矛盾环: %+v", res.Base)
	}
	checkCycleEvidence(t, in, res.Base.Cycle)
	if res.Ranges != nil {
		t.Fatalf("无解时不得返回范围: %+v", res.Ranges)
	}
}

// ---------- 请求校验 ----------

func validRangesRequest() RangesInput {
	return RangesInput{
		Input: Input{
			Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -1, Hi: 1}},
			Spans: []Span{
				{ID: "s1", Service: "a", Start: 0, End: 10},
				{ID: "s2", Service: "b", Parent: "s1", Start: 1, End: 2},
			},
		},
		Queries: []RangeQuery{{
			ID: "q", Left: TimePoint{Span: "s1", Point: "start"}, Right: TimePoint{Span: "s2", Point: "end"},
		}},
	}
}

func TestValidateRangesRequest(t *testing.T) {
	if errs := ValidateRangesRequest(validRangesRequest()); len(errs) != 0 {
		t.Fatalf("合法请求被误判: %v", errs)
	}
	cases := []struct {
		name string
		mut  func(*RangesInput)
	}{
		{"查询为空", func(r *RangesInput) { r.Queries = nil }},
		{"查询超量", func(r *RangesInput) {
			r.Queries = nil
			for i := 0; i < MaxQueries+1; i++ {
				r.Queries = append(r.Queries, RangeQuery{
					ID:    fmt.Sprintf("q%02d", i),
					Left:  TimePoint{Span: "s1", Point: "start"},
					Right: TimePoint{Span: "s2", Point: "end"},
				})
			}
		}},
		{"查询 ID 为空", func(r *RangesInput) { r.Queries[0].ID = "" }},
		{"查询 ID 重复", func(r *RangesInput) {
			r.Queries = append(r.Queries, RangeQuery{ID: "q",
				Left: TimePoint{Span: "s1", Point: "start"}, Right: TimePoint{Span: "s1", Point: "end"}})
		}},
		{"左 span 未知", func(r *RangesInput) { r.Queries[0].Left.Span = "ghost" }},
		{"右 span 为空", func(r *RangesInput) { r.Queries[0].Right.Span = "" }},
		{"时刻类型非法", func(r *RangesInput) { r.Queries[0].Right.Point = "middle" }},
		{"时刻类型为空", func(r *RangesInput) { r.Queries[0].Left.Point = "" }},
		{"底层输入非法(无锚点)", func(r *RangesInput) { r.Input.Services[0].Hi = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRangesRequest()
			tc.mut(&req)
			if errs := ValidateRangesRequest(req); len(errs) == 0 {
				t.Fatalf("应检出非法请求")
			}
		})
	}
}

// TestValidateRangesBoundaryCount 恰好 MaxQueries 个且 ID 唯一应通过。
func TestValidateRangesBoundaryCount(t *testing.T) {
	req := validRangesRequest()
	req.Queries = nil
	for i := 0; i < MaxQueries; i++ {
		req.Queries = append(req.Queries, RangeQuery{
			ID:    fmt.Sprintf("q%02d", i),
			Left:  TimePoint{Span: "s1", Point: "start"},
			Right: TimePoint{Span: "s2", Point: "end"},
		})
	}
	if errs := ValidateRangesRequest(req); len(errs) != 0 {
		t.Fatalf("恰好 %d 个查询应合法: %v", MaxQueries, errs)
	}
}
