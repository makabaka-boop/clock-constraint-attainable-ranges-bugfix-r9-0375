package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"clocksync/internal/solver"
)

func postRanges(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/ranges", strings.NewReader(body))
	rec := httptest.NewRecorder()
	New(nil).ServeHTTP(rec, req)
	return rec
}

const feasibleRangesBody = `{
  "input": {
    "services": [{"id":"a","lo":0,"hi":0},{"id":"b","lo":-10,"hi":10}],
    "spans": [
      {"id":"p","service":"a","parent":"","start":0,"end":100},
      {"id":"c","service":"b","parent":"p","start":0,"end":10}
    ]
  },
  "queries": [
    {"id":"z","left":{"span":"p","point":"start"},"right":{"span":"c","point":"start"}},
    {"id":"m","left":{"span":"c","point":"end"},"right":{"span":"p","point":"end"}}
  ]
}`

func TestRangesOK(t *testing.T) {
	rec := postRanges(t, feasibleRangesBody)
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
	if len(res.Ranges) != 2 || res.Ranges[0].ID != "z" || res.Ranges[1].ID != "m" {
		t.Fatalf("查询身份/顺序未保留: %+v", res.Ranges)
	}
	// z: D = x_b ∈ [0,10]；m: D = 100-10 - x_b = 90-x_b ∈ [80,90]。
	if res.Ranges[0].Min != 0 || res.Ranges[0].Max != 10 {
		t.Fatalf("z 端点错误: [%d,%d]", res.Ranges[0].Min, res.Ranges[0].Max)
	}
	if res.Ranges[1].Min != 80 || res.Ranges[1].Max != 90 {
		t.Fatalf("m 端点错误: [%d,%d]", res.Ranges[1].Min, res.Ranges[1].Max)
	}
	for _, tr := range res.Ranges {
		if len(tr.MinWitness) != 2 || len(tr.MaxWitness) != 2 {
			t.Fatalf("见证必须是全部服务的完整偏移: %+v", tr)
		}
		if tr.MinWitness["a"] != 0 || tr.MaxWitness["a"] != 0 {
			t.Fatalf("见证破坏锚点: %+v %+v", tr.MinWitness, tr.MaxWitness)
		}
	}
}

// 无解时 200 返回矛盾环，且不含任何范围字段。
func TestRangesInfeasibleOnlyCycle(t *testing.T) {
	body := `{
	  "input": {
	    "services": [{"id":"a","lo":0,"hi":0},{"id":"b","lo":8,"hi":20}],
	    "spans": [
	      {"id":"s1","service":"a","parent":"","start":0,"end":3},
	      {"id":"s2","service":"b","parent":"s1","start":0,"end":10}
	    ]
	  },
	  "queries": [{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s2","point":"start"}}]
	}`
	rec := postRanges(t, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["ranges"]; present {
		t.Fatalf("无解响应不得包含 ranges: %s", rec.Body)
	}
	base, ok := raw["base"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 base: %s", rec.Body)
	}
	if base["status"] != "infeasible" {
		t.Fatalf("期望 infeasible: %s", rec.Body)
	}
	if _, ok := base["cycle"].(map[string]any); !ok {
		t.Fatalf("缺少矛盾环: %s", rec.Body)
	}
	for _, k := range []string{"offsets", "spans", "constraints", "order"} {
		if _, present := base[k]; present {
			t.Fatalf("无解 base 不得包含字段 %q: %s", k, rec.Body)
		}
	}
}

func TestRangesRejection(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"非法时刻引用", `{
		  "input":{"services":[{"id":"a","lo":0,"hi":0},{"id":"b","lo":-1,"hi":1}],
		    "spans":[{"id":"s1","service":"a","start":0,"end":10}]},
		  "queries":[{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"ghost","point":"end"}}]}`},
		{"非法时刻类型", `{
		  "input":{"services":[{"id":"a","lo":0,"hi":0},{"id":"b","lo":-1,"hi":1}],
		    "spans":[{"id":"s1","service":"a","start":0,"end":10}]},
		  "queries":[{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"middle"}}]}`},
		{"重复查询ID", `{
		  "input":{"services":[{"id":"a","lo":0,"hi":0},{"id":"b","lo":-1,"hi":1}],
		    "spans":[{"id":"s1","service":"a","start":0,"end":10}]},
		  "queries":[
		    {"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"end"}},
		    {"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"end"}}]}`},
		{"查询为空", `{
		  "input":{"services":[{"id":"a","lo":0,"hi":0},{"id":"b","lo":-1,"hi":1}],"spans":[]},"queries":[]}`},
		{"查询超量", func() string {
			var qs strings.Builder
			qs.WriteString(`{"input":{"services":[{"id":"a","lo":0,"hi":0},{"id":"b","lo":-1,"hi":1}],` +
				`"spans":[{"id":"s1","service":"a","start":0,"end":10}]},"queries":[`)
			for i := 0; i < solver.MaxQueries+1; i++ {
				if i > 0 {
					qs.WriteByte(',')
				}
				qs.WriteString(`{"id":"q`)
				qs.WriteString(strconv.Itoa(i))
				qs.WriteString(`","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"end"}}`)
			}
			qs.WriteString(`]}`)
			return qs.String()
		}()},
		{"底层输入非法", `{
		  "input":{"services":[{"id":"a","lo":0,"hi":1},{"id":"b","lo":-1,"hi":1}],
		    "spans":[]},
		  "queries":[{"id":"q","left":{"span":"s1","point":"start"},"right":{"span":"s1","point":"end"}}]}`},
		{"未知字段", `{"input":{},"queries":[],"bogus":1}`},
		{"非法JSON", `{not json`},
		{"尾随内容", feasibleRangesBody + `{"x":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postRanges(t, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: 应整份拒绝（400），实际 %d: %s", tc.name, rec.Code, rec.Body)
			}
			var body struct {
				Errors []string `json:"errors"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) == 0 {
				t.Fatalf("%s: 应返回非空错误列表: %s", tc.name, rec.Body)
			}
			// 被拒时任何情况下都不得出现范围数据。
			if strings.Contains(rec.Body.String(), "minWitness") {
				t.Fatalf("%s: 拒绝响应不得携带范围/见证: %s", tc.name, rec.Body)
			}
		})
	}
}

func TestRangesPageServed(t *testing.T) {
	req := httptest.NewRequest("GET", "/ranges", nil)
	rec := httptest.NewRecorder()
	New(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("页面类型错误: %s", ct)
	}
	if !strings.Contains(rec.Body.String(), "调用时刻") {
		t.Fatalf("页面内容异常: %s", rec.Body.String()[:min(200, len(rec.Body.String()))])
	}
}
