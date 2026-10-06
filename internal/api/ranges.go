package api

import (
	"clocksync/internal/solver"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
)

//go:embed ranges.html
var rangesPage string

// handleRanges 复核时刻差可行范围。输入或任一查询非法时整份拒绝（400 +
// 错误列表）；输入本身无解时 200 返回，body 中只携带矛盾环。
func handleRanges(w http.ResponseWriter, r *http.Request) {
	var request solver.RangesInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []string{"请求体不是合法的输入 JSON: " + err.Error()},
		})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []string{"请求体在单个 JSON 对象之后还有多余内容"},
		})
		return
	}
	if errs := solver.ValidateRangesRequest(request); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": errs})
		return
	}
	result, err := solver.SolveRanges(request)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"errors": []string{err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveRanges(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, rangesPage)
}
