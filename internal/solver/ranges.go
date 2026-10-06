package solver

import "fmt"

type TimePoint struct {
	Span  string `json:"span"`
	Point string `json:"point"`
}
type RangeQuery struct {
	ID    string    `json:"id"`
	Left  TimePoint `json:"left"`
	Right TimePoint `json:"right"`
}
type RangesInput struct {
	Input   Input        `json:"input"`
	Queries []RangeQuery `json:"queries"`
}
type TimeRange struct {
	ID         string         `json:"id"`
	Min        int64          `json:"min"`
	Max        int64          `json:"max"`
	MinWitness map[string]int `json:"minWitness"`
	MaxWitness map[string]int `json:"maxWitness"`
}
type RangesResult struct {
	Base   Result      `json:"base"`
	Ranges []TimeRange `json:"ranges,omitempty"`
}

func queryPoint(spans map[string]Span, p TimePoint) (Span, int, error) {
	s, ok := spans[p.Span]
	if !ok {
		return Span{}, 0, fmt.Errorf("unknown span %q", p.Span)
	}
	switch p.Point {
	case "start":
		return s, s.Start, nil
	case "end":
		return s, s.End, nil
	default:
		return Span{}, 0, fmt.Errorf("invalid time point")
	}
}
func SolveRanges(request RangesInput) (RangesResult, error) {
	base, err := Solve(request.Input)
	if err != nil {
		return RangesResult{}, err
	}
	out := RangesResult{Base: base}
	if base.Status != "ok" {
		return out, nil
	}
	spans := map[string]Span{}
	services := map[string]Service{}
	for _, s := range request.Input.Spans {
		spans[s.ID] = s
	}
	for _, s := range request.Input.Services {
		services[s.ID] = s
	}
	for _, q := range request.Queries {
		left, l, err := queryPoint(spans, q.Left)
		if err != nil {
			return RangesResult{}, err
		}
		right, r, err := queryPoint(spans, q.Right)
		if err != nil {
			return RangesResult{}, err
		}
		ls, rs := services[left.Service], services[right.Service]
		out.Ranges = append(out.Ranges, TimeRange{q.ID, int64(r - l + rs.Lo - ls.Hi), int64(r - l + rs.Hi - ls.Lo), base.Offsets, base.Offsets})
	}
	return out, nil
}
