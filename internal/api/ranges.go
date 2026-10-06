package api

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"

	"clocksync/internal/solver"
)

// handleRanges 处理时刻差范围复核。请求未通过校验（输入非法、时刻引用
// 非法、查询重复或超限）时整份拒绝；求解成功时返回的每个范围都带有
// 确实达到端点的完整见证。
func handleRanges(w http.ResponseWriter, r *http.Request) {
	var request solver.RangesInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []string{"请求体不是合法的查询 JSON: " + err.Error()},
		})
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []string{"请求体含有多余内容"},
		})
		return
	}
	result, err := solver.SolveRanges(request)
	if err != nil {
		var reqErr *solver.RequestError
		if errors.As(err, &reqErr) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errors": reqErr.Problems})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"errors": []string{err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// serveRanges 输出前端构建产物中的范围复核页；产物缺失时明确提示，
// 不提供过时或伪造的页面。
func serveRanges(static fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if static != nil {
			if f, err := static.Open("ranges.html"); err == nil {
				defer f.Close()
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = io.Copy(w, f)
				return
			}
		}
		http.Error(w, "范围复核页未构建：请先在 web/ 执行 npm run build", http.StatusNotFound)
	}
}
