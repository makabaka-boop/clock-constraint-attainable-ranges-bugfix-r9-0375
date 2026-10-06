package solver

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// ---------- 穷举对拍 ----------

// bruteForceRange 穷举所有界内整数偏移向量，对可行向量计算查询的校正
// 差值，返回真实的最小/最大值。差值 = (r + x[rs]) - (l + x[ls])。
func bruteForceRange(in Input, q RangeQuery) (min, max int64, ok bool) {
	_, _, order, _ := buildGraph(in)
	svc := map[string]Service{}
	for _, s := range in.Services {
		svc[s.ID] = s
	}
	spans := map[string]Span{}
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	l := pointTime(spans[q.Left.Span], q.Left.Point)
	r := pointTime(spans[q.Right.Span], q.Right.Point)
	ls, rs := spans[q.Left.Span].Service, spans[q.Right.Span].Service
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	found := false
	cur := make([]int, len(order))
	var rec func(i int)
	rec = func(i int) {
		if i == len(order) {
			if !feasibleVec(in, order, cur) {
				return
			}
			d := int64(r - l + cur[pos[rs]] - cur[pos[ls]])
			if !found || d < min {
				min = d
			}
			if !found || d > max {
				max = d
			}
			found = true
			return
		}
		s := svc[order[i]]
		for v := s.Lo; v <= s.Hi; v++ {
			cur[i] = v
			rec(i + 1)
		}
	}
	rec(0)
	return min, max, found
}

// randQueries 生成 1~4 个引用合法、ID 唯一的随机查询。span 从输入中
// 随机取，因此自然覆盖同服务、同 span、跨服务等情形。
func randQueries(r *rand.Rand, in Input) []RangeQuery {
	n := 1 + r.Intn(4)
	qs := make([]RangeQuery, 0, n)
	point := func() TimePoint {
		sp := in.Spans[r.Intn(len(in.Spans))]
		p := "start"
		if r.Intn(2) == 1 {
			p = "end"
		}
		return TimePoint{Span: sp.ID, Point: p}
	}
	for i := 0; i < n; i++ {
		qs = append(qs, RangeQuery{ID: fmt.Sprintf("q%d", i), Left: point(), Right: point()})
	}
	return qs
}

func TestSolveRangesAgainstBruteForce(t *testing.T) {
	const iterations = 4000
	for seed := int64(0); seed < iterations; seed++ {
		r := rand.New(rand.NewSource(seed))
		in := randInput(r)
		queries := randQueries(r, in)
		res, err := SolveRanges(RangesInput{Input: in, Queries: queries})
		if err != nil {
			t.Fatalf("seed %d: SolveRanges 返回错误: %v", seed, err)
		}
		if res.Base.Status != "ok" {
			if len(res.Ranges) != 0 {
				t.Fatalf("seed %d: 无解时不得给出任何范围: %+v", seed, res.Ranges)
			}
			checkCycleEvidence(t, in, res.Base.Cycle)
			continue
		}
		if len(res.Ranges) != len(queries) {
			t.Fatalf("seed %d: 范围数量 %d 与查询数量 %d 不一致", seed, len(res.Ranges), len(queries))
		}
		for i, q := range queries {
			tr := res.Ranges[i]
			if tr.ID != q.ID || tr.Left != q.Left || tr.Right != q.Right {
				t.Fatalf("seed %d: 第 %d 个结果未保留查询身份/顺序: %+v vs %+v", seed, i, tr, q)
			}
			wantMin, wantMax, ok := bruteForceRange(in, q)
			if !ok {
				t.Fatalf("seed %d: 求解器可行但穷举无可行向量", seed)
			}
			if tr.Min != wantMin || tr.Max != wantMax {
				t.Fatalf("seed %d: 查询 %s 范围不一致 solver=[%d,%d] brute=[%d,%d]\n输入: %+v",
					seed, q.ID, tr.Min, tr.Max, wantMin, wantMax, in)
			}
			checkRangeWitness(t, in, q, tr.MinWitness, tr.Min)
			checkRangeWitness(t, in, q, tr.MaxWitness, tr.Max)
		}
	}
}

// checkRangeWitness 独立核验一份见证：覆盖全部服务、落在偏移界内、
// 满足全部父子约束，且确实达到声明的端点值。
func checkRangeWitness(t *testing.T, in Input, q RangeQuery, w map[string]int, endpoint int64) {
	t.Helper()
	_, _, order, _ := buildGraph(in)
	if len(w) != len(order) {
		t.Fatalf("查询 %s 的见证未覆盖全部服务: %+v", q.ID, w)
	}
	vec := make([]int, len(order))
	svc := map[string]Service{}
	for _, s := range in.Services {
		svc[s.ID] = s
	}
	for i, id := range order {
		v, ok := w[id]
		if !ok {
			t.Fatalf("查询 %s 的见证缺少服务 %s", q.ID, id)
		}
		if v < svc[id].Lo || v > svc[id].Hi {
			t.Fatalf("查询 %s 的见证中 %s=%d 超出偏移界 [%d,%d]", q.ID, id, v, svc[id].Lo, svc[id].Hi)
		}
		vec[i] = v
	}
	if !feasibleVec(in, order, vec) {
		t.Fatalf("查询 %s 的见证违反父子约束: %+v", q.ID, w)
	}
	spans := map[string]Span{}
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	l := pointTime(spans[q.Left.Span], q.Left.Point)
	r := pointTime(spans[q.Right.Span], q.Right.Point)
	got := int64(r+w[spans[q.Right.Span].Service]) - int64(l+w[spans[q.Left.Span].Service])
	if got != endpoint {
		t.Fatalf("查询 %s 的见证实际差值 %d 未达到声明端点 %d", q.ID, got, endpoint)
	}
}

// ---------- 定向用例 ----------

// 链式约束把差值锁死：b 的偏移界宽达 [-10,10]，但父子约束迫使 x_b == 0，
// 范围必须退化为 [0,0]——不能按裸偏移界给出 [-10,10]。
func TestRangesRespectCallChain(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -10, Hi: 10}},
		Spans: []Span{
			{ID: "p", Service: "a", Start: 0, End: 10},
			{ID: "c", Service: "b", Parent: "p", Start: 0, End: 10},
		},
	}
	q := RangeQuery{ID: "q", Left: TimePoint{Span: "p", Point: "start"}, Right: TimePoint{Span: "c", Point: "start"}}
	res, err := SolveRanges(RangesInput{Input: in, Queries: []RangeQuery{q}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Ranges) != 1 {
		t.Fatalf("应返回 1 个范围: %+v", res)
	}
	tr := res.Ranges[0]
	if tr.Min != 0 || tr.Max != 0 {
		t.Fatalf("调用链约束下范围应退化为 [0,0]，实际 [%d,%d]", tr.Min, tr.Max)
	}
	checkRangeWitness(t, in, q, tr.MinWitness, tr.Min)
	checkRangeWitness(t, in, q, tr.MaxWitness, tr.Max)
}

// 同一服务上的两个时刻共享同一偏移，差值是常数，不得展示额外变化。
func TestRangesSameServiceIsConstant(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -10, Hi: 10}},
		Spans: []Span{
			{ID: "p", Service: "a", Start: 0, End: 100},
			{ID: "c1", Service: "b", Start: 5, End: 9},
			{ID: "c2", Service: "b", Start: 20, End: 30},
		},
	}
	res, err := SolveRanges(RangesInput{Input: in, Queries: []RangeQuery{
		{ID: "q", Left: TimePoint{Span: "c1", Point: "end"}, Right: TimePoint{Span: "c2", Point: "start"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	tr := res.Ranges[0]
	if tr.Min != 11 || tr.Max != 11 {
		t.Fatalf("同服务差值应恒为 11，实际 [%d,%d]", tr.Min, tr.Max)
	}
}

// 允许负差值（右时刻早于左时刻）与相等时刻（min == max == 0）。
func TestRangesNegativeAndEqual(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -5, Hi: 3}},
		Spans: []Span{
			{ID: "p", Service: "a", Start: 0, End: 10},
			{ID: "c", Service: "b", Start: 0, End: 10},
		},
	}
	res, err := SolveRanges(RangesInput{Input: in, Queries: []RangeQuery{
		{ID: "neg", Left: TimePoint{Span: "p", Point: "start"}, Right: TimePoint{Span: "c", Point: "start"}},
		{ID: "eq", Left: TimePoint{Span: "p", Point: "start"}, Right: TimePoint{Span: "p", Point: "start"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Ranges) != 2 || res.Ranges[0].ID != "neg" || res.Ranges[1].ID != "eq" {
		t.Fatalf("结果应保持查询身份与顺序: %+v", res.Ranges)
	}
	neg := res.Ranges[0]
	if neg.Min != -5 || neg.Max != 3 {
		t.Fatalf("无约束时差值应覆盖整个偏移区间 [-5,3]，实际 [%d,%d]", neg.Min, neg.Max)
	}
	if neg.Min >= 0 {
		t.Fatalf("应允许负差值，实际 min=%d", neg.Min)
	}
	eq := res.Ranges[1]
	if eq.Min != 0 || eq.Max != 0 {
		t.Fatalf("同一时刻差值应为 [0,0]，实际 [%d,%d]", eq.Min, eq.Max)
	}
}

// 跨服务传递约束：x_c 被 b 的链路间接约束，范围不能按 c 的裸界计算。
func TestRangesTransitiveConstraints(t *testing.T) {
	in := Input{
		Services: []Service{
			{ID: "a", Lo: 0, Hi: 0},
			{ID: "b", Lo: -100, Hi: 100},
			{ID: "c", Lo: -100, Hi: 100},
		},
		Spans: []Span{
			{ID: "s1", Service: "a", Start: 0, End: 10},
			{ID: "s2", Service: "b", Parent: "s1", Start: 1, End: 9},
			{ID: "s3", Service: "c", Parent: "s2", Start: 3, End: 7},
		},
	}
	// x_b ∈ [-1,1]（相对 a），x_c-x_b ∈ [-2,2]，故 x_c ∈ [-3,3]，
	// 差值 s3.start - s1.start = 3 + x_c ∈ [0,6]。
	q := RangeQuery{ID: "q", Left: TimePoint{Span: "s1", Point: "start"}, Right: TimePoint{Span: "s3", Point: "start"}}
	res, err := SolveRanges(RangesInput{Input: in, Queries: []RangeQuery{q}})
	if err != nil {
		t.Fatal(err)
	}
	tr := res.Ranges[0]
	if tr.Min != 0 || tr.Max != 6 {
		t.Fatalf("传递约束下范围应为 [0,6]，实际 [%d,%d]", tr.Min, tr.Max)
	}
	checkRangeWitness(t, in, q, tr.MinWitness, tr.Min)
	checkRangeWitness(t, in, q, tr.MaxWitness, tr.Max)
}

// 原输入无解：只交付矛盾环证据，不给出任何范围。
func TestRangesInfeasibleOnlyCycle(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: 8, Hi: 20}},
		Spans: []Span{
			{ID: "s1", Service: "a", Start: 0, End: 3},
			{ID: "s2", Service: "b", Parent: "s1", Start: 0, End: 10},
		},
	}
	res, err := SolveRanges(RangesInput{Input: in, Queries: []RangeQuery{
		{ID: "q", Left: TimePoint{Span: "s1", Point: "start"}, Right: TimePoint{Span: "s2", Point: "start"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Base.Status != "infeasible" {
		t.Fatalf("期望无解: %+v", res.Base)
	}
	if len(res.Ranges) != 0 {
		t.Fatalf("无解时不得给出任何范围: %+v", res.Ranges)
	}
	checkCycleEvidence(t, in, res.Base.Cycle)
}

// 查询校验：非法时刻引用、重复 ID、空 ID、数量超限、输入非法，均整份拒绝。
func TestRangesQueryValidation(t *testing.T) {
	ok := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -1, Hi: 1}},
		Spans: []Span{
			{ID: "s1", Service: "a", Start: 0, End: 10},
			{ID: "s2", Service: "b", Parent: "s1", Start: 1, End: 2},
		},
	}
	okQuery := RangeQuery{ID: "q", Left: TimePoint{Span: "s1", Point: "start"}, Right: TimePoint{Span: "s2", Point: "end"}}
	cases := []struct {
		name    string
		mut     func(*RangesInput)
		wantSub string
	}{
		{"未知span", func(r *RangesInput) { r.Queries[0].Left.Span = "ghost" }, "不存在的 span"},
		{"右侧未知span", func(r *RangesInput) { r.Queries[0].Right.Span = "ghost" }, "不存在的 span"},
		{"非法时刻类型", func(r *RangesInput) { r.Queries[0].Left.Point = "middle" }, "非法"},
		{"空时刻类型", func(r *RangesInput) { r.Queries[0].Right.Point = "" }, "非法"},
		{"查询ID重复", func(r *RangesInput) {
			r.Queries = append(r.Queries, r.Queries[0])
		}, "重复"},
		{"查询ID为空", func(r *RangesInput) { r.Queries[0].ID = "" }, "不能为空"},
		{"查询超限", func(r *RangesInput) {
			for i := 0; i < MaxQueries; i++ {
				q := r.Queries[0]
				q.ID = fmt.Sprintf("q%d", i+1)
				r.Queries = append(r.Queries, q)
			}
		}, "至多"},
		{"输入非法", func(r *RangesInput) { r.Input.Services[1].Lo = 5 }, "下界"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := RangesInput{
				Input:   Input{Services: append([]Service(nil), ok.Services...), Spans: append([]Span(nil), ok.Spans...)},
				Queries: []RangeQuery{okQuery},
			}
			tc.mut(&req)
			_, err := SolveRanges(req)
			if err == nil {
				t.Fatalf("应整份拒绝")
			}
			reqErr, ok := err.(*RequestError)
			if !ok {
				t.Fatalf("应返回 RequestError，实际 %T: %v", err, err)
			}
			if len(reqErr.Problems) == 0 || !strings.Contains(reqErr.Error(), tc.wantSub) {
				t.Fatalf("错误信息应包含 %q: %v", tc.wantSub, reqErr)
			}
		})
	}
	// 边界：恰好 MaxQueries 个合法查询应被接受。
	req := RangesInput{Input: ok}
	for i := 0; i < MaxQueries; i++ {
		q := okQuery
		q.ID = fmt.Sprintf("q%d", i)
		req.Queries = append(req.Queries, q)
	}
	res, err := SolveRanges(req)
	if err != nil {
		t.Fatalf("恰好 %d 个合法查询应被接受: %v", MaxQueries, err)
	}
	if len(res.Ranges) != MaxQueries {
		t.Fatalf("应返回 %d 个范围: %d", MaxQueries, len(res.Ranges))
	}
	for i, tr := range res.Ranges {
		if tr.ID != fmt.Sprintf("q%d", i) {
			t.Fatalf("第 %d 个结果 ID 应为 q%d，实际 %s", i, i, tr.ID)
		}
	}
	// 零个查询同样合法：只交付 base 求解结果，不携带范围。
	res, err = SolveRanges(RangesInput{Input: ok})
	if err != nil {
		t.Fatalf("零查询应被接受: %v", err)
	}
	if res.Base.Status != "ok" || len(res.Ranges) != 0 {
		t.Fatalf("零查询应只返回 base: %+v", res)
	}
}
