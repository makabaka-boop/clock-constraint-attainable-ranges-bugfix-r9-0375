package solver

import (
	"math/rand"
	"sort"
	"testing"
)

// ---------- 穷举对拍 ----------

// bruteForceLexMin 穷举所有界内整数偏移向量，返回字典序最小可行解。
func bruteForceLexMin(in Input) ([]int, bool) {
	_, _, order, _ := buildGraph(in)
	svc := map[string]Service{}
	for _, s := range in.Services {
		svc[s.ID] = s
	}
	var best []int
	cur := make([]int, len(order))
	var rec func(i int)
	rec = func(i int) {
		if i == len(order) {
			if feasibleVec(in, order, cur) && (best == nil || lexLess(cur, best)) {
				best = append([]int(nil), cur...)
			}
			return
		}
		s := svc[order[i]]
		for v := s.Lo; v <= s.Hi; v++ {
			cur[i] = v
			rec(i + 1)
		}
	}
	rec(0)
	return best, best != nil
}

func feasibleVec(in Input, order []string, vec []int) bool {
	off := make(map[string]int, len(order))
	for i, id := range order {
		off[id] = vec[i]
	}
	spans := map[string]Span{}
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	for _, sp := range in.Spans {
		if sp.Parent == "" {
			continue
		}
		p := spans[sp.Parent]
		if sp.Start+off[sp.Service] < p.Start+off[p.Service] {
			return false
		}
		if sp.End+off[sp.Service] > p.End+off[p.Service] {
			return false
		}
	}
	return true
}

func lexLess(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// randInput 生成小规模随机输入：2~3 个服务（恰一个锚定）、1~6 个
// 构成森林的 span、窄偏移区间，保证穷举可行。
func randInput(r *rand.Rand) Input {
	nSvc := 2 + r.Intn(2)
	anchor := r.Intn(nSvc)
	in := Input{}
	for i := 0; i < nSvc; i++ {
		id := string(rune('a' + i))
		if i == anchor {
			in.Services = append(in.Services, Service{ID: id, Lo: 0, Hi: 0})
			continue
		}
		lo := -3 + r.Intn(6) // [-3,2]
		hi := lo + r.Intn(7-lo)
		if lo == 0 && hi == 0 {
			hi = 1 // 非锚定服务不得恰好是 [0,0]
		}
		in.Services = append(in.Services, Service{ID: id, Lo: lo, Hi: hi})
	}
	nSpans := 1 + r.Intn(6)
	for i := 0; i < nSpans; i++ {
		sp := Span{
			ID:      "s" + string(rune('0'+i)),
			Service: in.Services[r.Intn(nSvc)].ID,
			Start:   -6 + r.Intn(13),
		}
		sp.End = sp.Start + r.Intn(7)
		if i > 0 && r.Intn(4) > 0 {
			sp.Parent = in.Spans[r.Intn(i)].ID
		}
		in.Spans = append(in.Spans, sp)
	}
	return in
}

func TestSolveAgainstBruteForce(t *testing.T) {
	const iterations = 4000
	for seed := int64(0); seed < iterations; seed++ {
		r := rand.New(rand.NewSource(seed))
		in := randInput(r)
		if errs := Validate(in); len(errs) != 0 {
			t.Fatalf("seed %d: 生成器产生了非法输入: %v", seed, errs)
		}
		res, err := Solve(in)
		if err != nil {
			t.Fatalf("seed %d: Solve 返回错误: %v", seed, err)
		}
		best, ok := bruteForceLexMin(in)
		if ok != (res.Status == "ok") {
			t.Fatalf("seed %d: 可行性不一致 solver=%s brute=%v\n输入: %+v", seed, res.Status, ok, in)
		}
		if !ok {
			checkCycleEvidence(t, in, res.Cycle)
			continue
		}
		_, _, order, _ := buildGraph(in)
		for i, id := range order {
			if res.Offsets[id] != best[i] {
				t.Fatalf("seed %d: 字典序最小解不一致\nsolver=%v\nbrute =%v\n输入: %+v",
					seed, res.Offsets, best, in)
			}
		}
		checkSlacks(t, in, res)
	}
}

// ---------- 余量复核 ----------

func checkSlacks(t *testing.T, in Input, res Result) {
	t.Helper()
	spans := map[string]Span{}
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	seen := map[string]bool{}
	for _, c := range res.Constraints {
		sp := spans[c.Span]
		p := spans[c.ParentSpan]
		if sp.ID == "" || p.ID == "" || sp.Parent != p.ID {
			t.Fatalf("余量条目引用了不存在的父子关系: %+v", c)
		}
		seen[c.Span] = true
		wantStart := (sp.Start + res.Offsets[sp.Service]) - (p.Start + res.Offsets[p.Service])
		wantEnd := (p.End + res.Offsets[p.Service]) - (sp.End + res.Offsets[sp.Service])
		if c.StartSlack != wantStart || c.EndSlack != wantEnd {
			t.Fatalf("span %s 余量错误: got (%d,%d) want (%d,%d)",
				sp.ID, c.StartSlack, c.EndSlack, wantStart, wantEnd)
		}
		if c.StartSlack < 0 || c.EndSlack < 0 {
			t.Fatalf("span %s 余量为负: %+v", sp.ID, c)
		}
	}
	for _, sp := range in.Spans {
		if sp.Parent != "" && !seen[sp.ID] {
			t.Fatalf("缺少 span %s 的余量条目", sp.ID)
		}
	}
}

// ---------- 矛盾环逐边核验 ----------

// checkCycleEvidence 不依赖求解器内部结构，直接从原始输入独立重算每条
// 边的端点与权重，验证环确实由真实约束构成、首尾相接且总权重为负。
func checkCycleEvidence(t *testing.T, in Input, cyc *Cycle) {
	t.Helper()
	if cyc == nil {
		t.Fatal("无解但未给出矛盾环")
	}
	if len(cyc.Edges) == 0 {
		t.Fatal("矛盾环为空")
	}
	svc := map[string]Service{}
	for _, s := range in.Services {
		svc[s.ID] = s
	}
	spans := map[string]Span{}
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	sum := 0
	for i, ce := range cyc.Edges {
		next := cyc.Edges[(i+1)%len(cyc.Edges)]
		if ce.To != next.From {
			t.Fatalf("第 %d 条边 %s->%s 与下一条 %s->%s 不相接", i, ce.From, ce.To, next.From, next.To)
		}
		sum += ce.Weight
		checkEdgeAgainstInput(t, i, ce, svc, spans)
	}
	if sum != cyc.TotalWeight {
		t.Fatalf("环总权重不一致: 边求和=%d 报告=%d", sum, cyc.TotalWeight)
	}
	if sum >= 0 {
		t.Fatalf("矛盾环总权重必须 < 0，实际 %d", sum)
	}
}

// checkEdgeAgainstInput 按边声明的来源，从原始输入独立重算权重与端点。
func checkEdgeAgainstInput(t *testing.T, i int, ce CycleEdge, svc map[string]Service, spans map[string]Span) {
	t.Helper()
	var wantFrom, wantTo string
	var wantW int
	switch ce.Ref.Kind {
	case KindBoundUpper:
		s, ok := svc[ce.Ref.Service]
		if !ok {
			t.Fatalf("第 %d 条边引用未知服务 %q", i, ce.Ref.Service)
		}
		wantFrom, wantTo, wantW = ZeroLabel, s.ID, s.Hi
	case KindBoundLower:
		s, ok := svc[ce.Ref.Service]
		if !ok {
			t.Fatalf("第 %d 条边引用未知服务 %q", i, ce.Ref.Service)
		}
		wantFrom, wantTo, wantW = s.ID, ZeroLabel, -s.Lo
	case KindChildStart:
		sp, p := spans[ce.Ref.Span], spans[ce.Ref.ParentSpan]
		if sp.ID == "" || p.ID == "" || sp.Parent != p.ID {
			t.Fatalf("第 %d 条边引用了不存在的父子关系 %+v", i, ce.Ref)
		}
		wantFrom, wantTo, wantW = sp.Service, p.Service, sp.Start-p.Start
	case KindChildEnd:
		sp, p := spans[ce.Ref.Span], spans[ce.Ref.ParentSpan]
		if sp.ID == "" || p.ID == "" || sp.Parent != p.ID {
			t.Fatalf("第 %d 条边引用了不存在的父子关系 %+v", i, ce.Ref)
		}
		wantFrom, wantTo, wantW = p.Service, sp.Service, p.End-sp.End
	default:
		t.Fatalf("第 %d 条边来源类型未知: %q", i, ce.Ref.Kind)
	}
	if ce.From != wantFrom || ce.To != wantTo || ce.Weight != wantW {
		t.Fatalf("第 %d 条边与输入不符: 环中 (%s->%s w=%d)，由输入重算 (%s->%s w=%d)",
			i, ce.From, ce.To, ce.Weight, wantFrom, wantTo, wantW)
	}
}

// ---------- 定向用例 ----------

func TestLexMinHandcrafted(t *testing.T) {
	// a 锚定；b 的区间 [-10,10]，子本地起点比父起点早 3，故 x_b >= 3。
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -10, Hi: 10}},
		Spans: []Span{
			{ID: "s1", Service: "a", Start: 0, End: 100},
			{ID: "s2", Service: "b", Parent: "s1", Start: -3, End: 0},
		},
	}
	res, err := Solve(in)
	if err != nil || res.Status != "ok" {
		t.Fatalf("期望可行: %+v err=%v", res, err)
	}
	if res.Offsets["b"] != 3 {
		t.Fatalf("x_b 应取最小可行值 3，实际 %d", res.Offsets["b"])
	}
	// 约束去掉后应退到下界 -10。
	in.Spans = in.Spans[:1]
	res, _ = Solve(in)
	if res.Offsets["b"] != -10 {
		t.Fatalf("无约束时 x_b 应取下界 -10，实际 %d", res.Offsets["b"])
	}
}

func TestLexMinRespectsServiceOrder(t *testing.T) {
	// 字典序按服务 ID：x_aa 先最小化。窗口宽松，两服务均应取到下界。
	in := Input{
		Services: []Service{
			{ID: "aa", Lo: -5, Hi: 5},
			{ID: "mm", Lo: 0, Hi: 0},
			{ID: "zz", Lo: -5, Hi: 5},
		},
		Spans: []Span{
			{ID: "s1", Service: "mm", Start: 0, End: 100},
			{ID: "s2", Service: "aa", Parent: "s1", Start: 10, End: 20},
			{ID: "s3", Service: "zz", Parent: "s1", Start: 10, End: 20},
		},
	}
	res, err := Solve(in)
	if err != nil || res.Status != "ok" {
		t.Fatalf("期望可行: %+v err=%v", res, err)
	}
	if res.Offsets["aa"] != -5 || res.Offsets["zz"] != -5 || res.Offsets["mm"] != 0 {
		t.Fatalf("两服务互不干扰，均应取下界: %+v", res.Offsets)
	}
	// 现在让 aa 与 zz 竞争：zz 的父是 aa 的 span，窗口与父完全等长。
	in.Spans = append(in.Spans, Span{ID: "s4", Service: "zz", Parent: "s2", Start: 10, End: 20})
	res, err = Solve(in)
	if err != nil || res.Status != "ok" {
		t.Fatalf("期望可行: %+v err=%v", res, err)
	}
	// x_aa 最小化到 -5 后，zz 需满足 x_zz >= x_aa + (10-10) 且
	// x_zz <= x_aa + (20-20) => x_zz == x_aa == -5。
	if res.Offsets["aa"] != -5 || res.Offsets["zz"] != -5 {
		t.Fatalf("字典序最小应为 aa=-5, zz=-5，实际 %+v", res.Offsets)
	}
}

func TestSelfLoopInfeasible(t *testing.T) {
	// 同服务父子：本地时钟下子起点已早于父起点，无法校正。
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -5, Hi: 5}},
		Spans: []Span{
			{ID: "s1", Service: "b", Start: 10, End: 20},
			{ID: "s2", Service: "b", Parent: "s1", Start: 5, End: 15},
		},
	}
	res, err := Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "infeasible" {
		t.Fatalf("期望无解: %+v", res)
	}
	checkCycleEvidence(t, in, res.Cycle)
	if len(res.Cycle.Edges) != 1 || res.Cycle.Edges[0].Ref.Kind != KindChildStart {
		t.Fatalf("应为长度 1 的 child_start 自环: %+v", res.Cycle)
	}
}

func TestInfeasibleThroughZeroNode(t *testing.T) {
	// 环经过零点节点：x_b >= 8（界）与 x_b <= 3（经 a 锚点传导）冲突。
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: 8, Hi: 20}},
		Spans: []Span{
			{ID: "s1", Service: "a", Start: 0, End: 3},
			{ID: "s2", Service: "b", Parent: "s1", Start: 0, End: 10},
		},
	}
	res, err := Solve(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "infeasible" {
		t.Fatalf("期望无解: %+v", res)
	}
	checkCycleEvidence(t, in, res.Cycle)
}

func TestNoSpans(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -2, Hi: 7}},
	}
	res, err := Solve(in)
	if err != nil || res.Status != "ok" {
		t.Fatalf("期望可行: %+v err=%v", res, err)
	}
	if res.Offsets["b"] != -2 {
		t.Fatalf("无约束时应取下界 -2，实际 %d", res.Offsets["b"])
	}
	if len(res.Constraints) != 0 || len(res.Spans) != 0 {
		t.Fatalf("无 span 时不应有校正数据: %+v", res)
	}
}

// ---------- 校验 ----------

func TestValidate(t *testing.T) {
	ok := Input{
		Services: []Service{{ID: "a", Lo: 0, Hi: 0}, {ID: "b", Lo: -1, Hi: 1}},
		Spans: []Span{
			{ID: "s1", Service: "a", Start: 0, End: 10},
			{ID: "s2", Service: "b", Parent: "s1", Start: 1, End: 2},
		},
	}
	if errs := Validate(ok); len(errs) != 0 {
		t.Fatalf("合法输入被误判: %v", errs)
	}
	cases := []struct {
		name string
		mut  func(*Input)
	}{
		{"服务过少", func(in *Input) { in.Services = in.Services[:1] }},
		{"服务过多", func(in *Input) {
			for i := 0; i < 7; i++ {
				in.Services = append(in.Services, Service{ID: string(rune('c' + i)), Lo: 0, Hi: 1})
			}
		}},
		{"无锚定", func(in *Input) { in.Services[0].Hi = 1 }},
		{"双锚定", func(in *Input) { in.Services[1].Lo, in.Services[1].Hi = 0, 0 }},
		{"界颠倒", func(in *Input) { in.Services[1].Lo, in.Services[1].Hi = 5, -5 }},
		{"服务ID重复", func(in *Input) { in.Services[1].ID = "a" }},
		{"服务ID为空", func(in *Input) { in.Services[1].ID = "" }},
		{"span过多", func(in *Input) {
			for i := 0; i < 101; i++ {
				in.Spans = append(in.Spans, Span{ID: "x" + string(rune(i)), Service: "a", Start: 0, End: 1})
			}
		}},
		{"spanID重复", func(in *Input) { in.Spans[1].ID = "s1" }},
		{"spanID为空", func(in *Input) { in.Spans[1].ID = "" }},
		{"未知服务", func(in *Input) { in.Spans[1].Service = "zz" }},
		{"起终颠倒", func(in *Input) { in.Spans[0].Start, in.Spans[0].End = 9, 1 }},
		{"父span不存在", func(in *Input) { in.Spans[1].Parent = "ghost" }},
		{"父链成环", func(in *Input) {
			in.Spans[0].Parent = "s2"
			in.Spans[1].Parent = "s1"
		}},
		{"量级超限", func(in *Input) { in.Spans[0].End = maxAbs + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := ok
			in.Services = append([]Service(nil), ok.Services...)
			in.Spans = append([]Span(nil), ok.Spans...)
			tc.mut(&in)
			if errs := Validate(in); len(errs) == 0 {
				t.Fatalf("应检出非法输入")
			}
		})
	}
}

// 排序稳定性：order 必须是服务 ID 的升序。
func TestOrderSorted(t *testing.T) {
	in := Input{
		Services: []Service{{ID: "zz", Lo: -1, Hi: 1}, {ID: "aa", Lo: 0, Hi: 0}},
	}
	_, _, order, _ := buildGraph(in)
	if !sort.StringsAreSorted(order) || order[0] != "aa" {
		t.Fatalf("order 应为 ID 升序: %v", order)
	}
}
