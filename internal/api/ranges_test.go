package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"clocksync/internal/solver"
)

func postRanges(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/ranges", strings.NewReader(body))
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)
	return rec
}

// 演示输入：b 的偏移被父子链约束到 [-2,2]；u 是 b 上的自由 span。
const rangesDemoRequest = `{
	"input": {
		"services": [{"id":"a","lo":0,"hi":0},{"id":"b","lo":-10,"hi":10}],
		"spans": [
			{"id":"p","service":"a","parent":"","start":0,"end":10},
			{"id":"c","service":"b","parent":"p","start":2,"end":8},
			{"id":"u","service":"b","parent":"","start":1,"end":3}
		]
	},
	"queries": [
		{"id":"q-chain","left":{"span":"p","point":"start"},"right":{"span":"c","point":"start"}},
		{"id":"q-free","left":{"span":"p","point":"start"},"right":{"span":"u","point":"start"}},
		{"id":"q-same-service","left":{"span":"c","point":"start"},"right":{"span":"u","point":"end"}}
	]
}`

func TestRangesOK(t *testing.T) {
	rec := postRanges(t, rangesDemoRequest)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body)
	}
	var res solver.RangesResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Base.Status != "ok" {
		t.Fatalf("期望 ok: %s", rec.Body)
	}
	// 查询身份与顺序必须保留。
	wantIDs := []string{"q-chain", "q-free", "q-same-service"}
	if len(res.Ranges) != len(wantIDs) {
		t.Fatalf("范围数量 %d，期望 %d", len(res.Ranges), len(wantIDs))
	}
	for i, id := range wantIDs {
		if res.Ranges[i].ID != id {
			t.Fatalf("第 %d 个结果 ID 应为 %s，实际 %s", i, id, res.Ranges[i].ID)
		}
	}
	// 端点期望值：链上查询被约束到 [0,4]；自由 span 经服务偏移间接
	// 约束到 [-1,3]；同服务查询恒为 1。
	wantRanges := [][2]int64{{0, 4}, {-1, 3}, {1, 1}}
	for i, want := range wantRanges {
		tr := res.Ranges[i]
		if tr.Min != want[0] || tr.Max != want[1] {
			t.Fatalf("%s 应为 [%d,%d]，实际 [%d,%d]", tr.ID, want[0], want[1], tr.Min, tr.Max)
		}
	}
	// 从响应与请求原文独立复验每份见证。
	var req solver.RangesInput
	if err := json.Unmarshal([]byte(rangesDemoRequest), &req); err != nil {
		t.Fatal(err)
	}
	for i, tr := range res.Ranges {
		checkAPIWitness(t, req.Input, req.Queries[i], tr.MinWitness, tr.Min)
		checkAPIWitness(t, req.Input, req.Queries[i], tr.MaxWitness, tr.Max)
	}
}

// checkAPIWitness 不经过求解器，直接从原始输入核验见证：覆盖全部服务、
// 落在偏移界内、满足全部父子约束、确实达到声明端点。
func checkAPIWitness(t *testing.T, in solver.Input, q solver.RangeQuery, w map[string]int, endpoint int64) {
	t.Helper()
	svc := map[string]solver.Service{}
	for _, s := range in.Services {
		svc[s.ID] = s
	}
	if len(w) != len(in.Services) {
		t.Fatalf("查询 %s 的见证未覆盖全部服务: %+v", q.ID, w)
	}
	for id, s := range svc {
		v, ok := w[id]
		if !ok {
			t.Fatalf("查询 %s 的见证缺少服务 %s", q.ID, id)
		}
		if v < s.Lo || v > s.Hi {
			t.Fatalf("查询 %s 的见证 %s=%d 超出偏移界 [%d,%d]", q.ID, id, v, s.Lo, s.Hi)
		}
	}
	spans := map[string]solver.Span{}
	for _, sp := range in.Spans {
		spans[sp.ID] = sp
	}
	for _, sp := range in.Spans {
		if sp.Parent == "" {
			continue
		}
		p := spans[sp.Parent]
		if sp.Start+w[sp.Service] < p.Start+w[p.Service] {
			t.Fatalf("查询 %s 的见证违反 %s 的起点约束", q.ID, sp.ID)
		}
		if sp.End+w[sp.Service] > p.End+w[p.Service] {
			t.Fatalf("查询 %s 的见证违反 %s 的终点约束", q.ID, sp.ID)
		}
	}
	local := func(tp solver.TimePoint) int {
		if tp.Point == "start" {
			return spans[tp.Span].Start
		}
		return spans[tp.Span].End
	}
	got := int64(local(q.Right)+w[spans[q.Right.Span].Service]) -
		int64(local(q.Left)+w[spans[q.Left.Span].Service])
	if got != endpoint {
		t.Fatalf("查询 %s 的见证实际差值 %d 未达到声明端点 %d", q.ID, got, endpoint)
	}
}

// 非法时刻引用、重复查询、超限、输入非法：一律整份拒绝（400）。
func TestRangesRejectsInvalidRequests(t *testing.T) {
	base := `{"input":{"services":[{"id":"a","lo":0,"hi":0},{"id":"b","lo":-1,"hi":1}],
		"spans":[{"id":"s1","service":"a","parent":"","start":0,"end":10},
		{"id":"s2","service":"b","parent":"s1","start":1,"end":2}]},%s}`
	cases := []struct {
		name string
		body string
	}{
		{"未知span", `"queries":[{"id":"q","left":{"span":"ghost","point":"start"},"right":{"span":"s1","point":"start"}}]`},
		{"非法时刻类型", `"queries":[{"id":"q","left":{"span":"s1","point":"middle"},"right":{"span":"s1","point":"start"}}]`},
		{"查询ID重复", `"queries":[{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"start"}},
			{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s2","point":"start"}}]`},
		{"查询ID为空", `"queries":[{"id":"","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"start"}}]`},
		{"查询超限", `"queries":[` + strings.Repeat(`{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"start"}},`, 16) +
			`{"id":"q17","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"start"}}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postRanges(t, fmt.Sprintf(base, tc.body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应整份拒绝（400），实际 %d: %s", rec.Code, rec.Body)
			}
			var body struct {
				Errors []string `json:"errors"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) == 0 {
				t.Fatalf("应返回错误列表: %s", rec.Body)
			}
		})
	}
	// 输入本身非法（缺锚定服务）。
	rec := postRanges(t, `{"input":{"services":[{"id":"a","lo":0,"hi":1},{"id":"b","lo":-1,"hi":1}],"spans":[]},"queries":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法输入应整份拒绝，实际 %d: %s", rec.Code, rec.Body)
	}
}

// 无解：只返回矛盾环，响应中不得出现任何范围字段。
func TestRangesInfeasibleHasNoRanges(t *testing.T) {
	rec := postRanges(t, `{
		"input": {
			"services": [{"id":"a","lo":0,"hi":0},{"id":"b","lo":8,"hi":20}],
			"spans": [
				{"id":"s1","service":"a","parent":"","start":0,"end":3},
				{"id":"s2","service":"b","parent":"s1","start":0,"end":10}
			]
		},
		"queries": [{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s2","point":"start"}}]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["ranges"]; present {
		t.Fatalf("无解响应不得包含 ranges 字段: %s", rec.Body)
	}
	base, ok := raw["base"].(map[string]any)
	if !ok || base["status"] != "infeasible" {
		t.Fatalf("base 应为 infeasible: %s", rec.Body)
	}
	for _, k := range []string{"offsets", "spans", "constraints", "order"} {
		if _, present := base[k]; present {
			t.Fatalf("无解时 base 不得包含字段 %q: %s", k, rec.Body)
		}
	}
	cyc, ok := base["cycle"].(map[string]any)
	if !ok {
		t.Fatalf("缺少矛盾环: %s", rec.Body)
	}
	if w, _ := cyc["totalWeight"].(float64); w >= 0 {
		t.Fatalf("矛盾环总权重应为负: %v", w)
	}
	if edges, _ := cyc["edges"].([]any); len(edges) == 0 {
		t.Fatalf("矛盾环为空: %s", rec.Body)
	}
}

func TestRangesRejectsUnknownFields(t *testing.T) {
	rec := postRanges(t, `{"input":{"services":[],"spans":[]},"queries":[],"bogus":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应被拒绝，状态码 %d", rec.Code)
	}
}

func TestRangesRejectsTrailingContent(t *testing.T) {
	rec := postRanges(t, `{"input":{"services":[],"spans":[]},"queries":[]} {}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("多余内容应被拒绝，状态码 %d", rec.Code)
	}
}

// 范围复核页由前端构建产物提供；产物缺失时明确 404 而不是伪造页面。
func TestServeRangesPage(t *testing.T) {
	static := fstest.MapFS{
		"ranges.html": &fstest.MapFile{Data: []byte("<html>ranges</html>")},
	}
	req := httptest.NewRequest("GET", "/ranges", nil)
	rec := httptest.NewRecorder()
	New(static).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type 应为 text/html，实际 %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "ranges") {
		t.Fatalf("应输出构建产物页面: %s", rec.Body)
	}

	rec2 := httptest.NewRecorder()
	New(nil).ServeHTTP(rec2, httptest.NewRequest("GET", "/ranges", nil))
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("产物缺失时应 404，实际 %d", rec2.Code)
	}
}
