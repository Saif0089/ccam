package panel

import (
	"net/http"
	"strings"
)

// Limit is one configured quota (a mirror of the row the Postgres layer stores).
// A limit caps a person, or the whole org, to a number of weighted tokens and/or
// a USD amount within a calendar window that resets daily, weekly, or monthly.
type Limit struct {
	ID          string   `json:"id"`
	SubjectType string   `json:"subjectType"` // 'person' | 'org'
	SubjectID   string   `json:"subjectId"`   // '' for org-wide
	WindowKind  string   `json:"windowKind"`  // 'day' | 'week' | 'month'
	MaxWeighted *float64 `json:"maxWeighted,omitempty"`
	MaxCostUSD  *float64 `json:"maxCostUsd,omitempty"`
	// MaxPercent caps a person's share of the account's weekly window — the
	// framing /usage uses ("25% of weekly"). 0..1. Only meaningful for a weekly
	// window; enforced from the real utilisation the gateway captures, so it does
	// nothing until that data is flowing (fail-open).
	MaxPercent *float64 `json:"maxPercent,omitempty"`
}

func (s *Server) handleListLimits(w http.ResponseWriter, r *http.Request) {
	limits, err := s.usage.ListLimits(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "The quotas could not be read: "+err.Error())
		return
	}
	d, _ := s.store.Load()
	out := make([]map[string]any, 0, len(limits))
	for _, l := range limits {
		name := "the whole team"
		if l.SubjectType == "person" {
			name = s.subjectName(d, "person", l.SubjectID)
		}
		// How much of this limit is used right now, so the board can flag the ones
		// approaching their cap. Best-effort: a usage read that errors just leaves
		// the row without a live figure rather than failing the whole list.
		frac, reset, _ := s.usage.LimitUsage(r.Context(), l)
		out = append(out, map[string]any{"limit": l, "name": name, "fraction": frac, "resetAt": reset})
	}
	writeJSON(w, http.StatusOK, map[string]any{"limits": out})
}

func (s *Server) handleSetLimit(w http.ResponseWriter, r *http.Request) {
	var l Limit
	if err := readJSON(r, &l); err != nil {
		fail(w, http.StatusBadRequest, "That request could not be read.")
		return
	}
	l.SubjectType = strings.ToLower(strings.TrimSpace(l.SubjectType))
	l.WindowKind = strings.ToLower(strings.TrimSpace(l.WindowKind))
	if l.SubjectType != "person" && l.SubjectType != "org" {
		fail(w, http.StatusBadRequest, "A quota applies to a person or to the whole team.")
		return
	}
	if l.SubjectType == "org" {
		l.SubjectID = ""
	}
	// A "% of weekly" cap is a person's share of the weekly window, so it forces
	// a weekly window and a person subject. Accept 0..1 or a 0..100 percentage.
	if l.MaxPercent != nil {
		p := *l.MaxPercent
		if p > 1 {
			p /= 100
		}
		if p <= 0 || p > 1 {
			fail(w, http.StatusBadRequest, "A weekly-share quota is a percent between 0 and 100.")
			return
		}
		if l.SubjectType != "person" {
			fail(w, http.StatusBadRequest, "A % of the weekly window applies to a person, not the whole team.")
			return
		}
		l.WindowKind = "week"
		l.MaxPercent = &p
	}
	switch l.WindowKind {
	case "day", "week", "month":
	default:
		fail(w, http.StatusBadRequest, "A quota resets daily, weekly, or monthly.")
		return
	}
	if l.MaxWeighted == nil && l.MaxCostUSD == nil && l.MaxPercent == nil {
		fail(w, http.StatusBadRequest, "Set a % of the weekly window, a weighted-token cap, or a USD cap.")
		return
	}
	if err := s.usage.SetLimit(r.Context(), l); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteLimit(w http.ResponseWriter, r *http.Request) {
	if err := s.usage.DeleteLimit(r.Context(), r.PathValue("id")); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
