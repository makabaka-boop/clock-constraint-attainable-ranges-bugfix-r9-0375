// Package api 提供时钟偏移校正复核的 HTTP 接口与静态页面托管。
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"clocksync/internal/solver"
)

// New 组装路由。static 为前端构建产物（可为 nil，则不托管页面）。
func New(static fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/solve", handleSolve)
	mux.HandleFunc("POST /api/ranges", handleRanges)
	mux.HandleFunc("GET /ranges", serveRanges)
	mux.HandleFunc("GET /api/example", handleExample)
	if static != nil {
		mux.Handle("GET /", http.FileServerFS(static))
	}
	return mux
}

func handleSolve(w http.ResponseWriter, r *http.Request) {
	var in solver.Input
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []string{"请求体不是合法的输入 JSON: " + err.Error()},
		})
		return
	}
	if errs := solver.Validate(in); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": errs})
		return
	}
	res, err := solver.Solve(in)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"errors": []string{err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleExample 返回示例输入：?kind=infeasible 时为一组必然矛盾的输入。
func handleExample(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("kind") == "infeasible" {
		writeJSON(w, http.StatusOK, infeasibleExample())
		return
	}
	writeJSON(w, http.StatusOK, feasibleExample())
}

// feasibleExample 是一条四服务的调用链，各服务本地时钟有偏差，
// 偏移界足够宽，存在可行校正。
func feasibleExample() solver.Input {
	return solver.Input{
		Services: []solver.Service{
			{ID: "auth", Lo: -50, Hi: 50},
			{ID: "gw", Lo: 0, Hi: 0},
			{ID: "orders", Lo: -80, Hi: 80},
			{ID: "pay", Lo: -120, Hi: 120},
		},
		Spans: []solver.Span{
			{ID: "s1", Service: "gw", Start: 100, End: 900},
			{ID: "s2", Service: "auth", Parent: "s1", Start: 150, End: 300},
			{ID: "s3", Service: "orders", Parent: "s1", Start: 400, End: 850},
			{ID: "s4", Service: "pay", Parent: "s3", Start: 500, End: 700},
			{ID: "s5", Service: "auth", Parent: "s1", Start: 320, End: 380},
		},
	}
}

// infeasibleExample 中 b、c 的本地时间迫使偏移逐级抬升，最终与锚点 a
// 冲突，构成经过零点的矛盾环。
func infeasibleExample() solver.Input {
	return solver.Input{
		Services: []solver.Service{
			{ID: "a", Lo: 0, Hi: 0},
			{ID: "b", Lo: -100, Hi: 100},
			{ID: "c", Lo: -100, Hi: 100},
		},
		Spans: []solver.Span{
			{ID: "s1", Service: "a", Start: 10, End: 1000},
			{ID: "s2", Service: "b", Parent: "s1", Start: 5, End: 900},
			{ID: "s3", Service: "c", Parent: "s2", Start: 0, End: 800},
			{ID: "s4", Service: "a", Parent: "s3", Start: -5, End: 700},
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
