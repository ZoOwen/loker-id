package server

import (
	"net/http"
	"testing"
)

func TestHandleStats(t *testing.T) {
	ts, st, _, _ := newTestServer(t)
	sourceID := mustSourceID(t, st, "kalibrr")

	seedJobWithStack(t, st, sourceID, "Acme", "Backend Engineer", "https://kalibrr.com/jobs/stats-1", []string{"Go"})
	seedJobWithStack(t, st, sourceID, "Acme", "Frontend Engineer", "https://kalibrr.com/jobs/stats-2", []string{"React"})

	var body statsResponse
	resp := getJSON(t, ts.URL+"/api/stats", &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if body.TotalActive != 2 {
		t.Errorf("TotalActive = %d, want 2", body.TotalActive)
	}

	var kalibrr *sourceStatResponse
	for i := range body.BySource {
		if body.BySource[i].Slug == "kalibrr" {
			kalibrr = &body.BySource[i]
		}
	}
	if kalibrr == nil {
		t.Fatal("kalibrr missing from by_source")
	}
	if kalibrr.ActiveJobs != 2 {
		t.Errorf("kalibrr ActiveJobs = %d, want 2", kalibrr.ActiveJobs)
	}

	stackCounts := map[string]int64{}
	for _, s := range body.ByStack {
		stackCounts[s.Stack] = s.JobCount
	}
	if stackCounts["Go"] != 1 || stackCounts["React"] != 1 {
		t.Errorf("ByStack = %+v, want Go=1 React=1", body.ByStack)
	}
}
