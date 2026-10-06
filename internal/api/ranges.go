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

func handleRanges(w http.ResponseWriter, r *http.Request) {
	var request solver.RangesInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "trailing content"})
		return
	}
	result, err := solver.SolveRanges(request)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, result)
}
func serveRanges(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, rangesPage)
}
