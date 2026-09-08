package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func getJSON(t *testing.T, url string, v any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("decode response from %s: %v", url, err)
		}
	}
	return resp
}

func TestHandleListJobs_ReturnsSeededJobsWithCompanyName(t *testing.T) {
	ts, st, _, _ := newTestServer(t)
	sourceID := mustSourceID(t, st, "kalibrr")

	seedJob(t, st, sourceID, "PT Gojek Indonesia", "Backend Engineer", "https://kalibrr.com/jobs/1")

	var body listJobsResponse
	resp, err := http.Get(ts.URL + "/api/jobs")
	if err != nil {
		t.Fatalf("GET /api/jobs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(body.Jobs) != 1 {
		t.Fatalf("len(Jobs) = %d, want 1", len(body.Jobs))
	}
	if body.Jobs[0].Title != "Backend Engineer" {
		t.Errorf("Title = %q, want %q", body.Jobs[0].Title, "Backend Engineer")
	}
	if body.Jobs[0].Company != "PT Gojek Indonesia" {
		t.Errorf("Company = %q, want %q", body.Jobs[0].Company, "PT Gojek Indonesia")
	}
	if body.NextCursor != nil {
		t.Errorf("NextCursor = %v, want nil (only one page)", *body.NextCursor)
	}
}

func TestHandleListJobs_FiltersByStack(t *testing.T) {
	ts, st, _, _ := newTestServer(t)
	sourceID := mustSourceID(t, st, "kalibrr")

	seedJobWithStack(t, st, sourceID, "Acme", "Go Dev", "https://kalibrr.com/jobs/go", []string{"Go"})
	seedJobWithStack(t, st, sourceID, "Acme", "PHP Dev", "https://kalibrr.com/jobs/php", []string{"PHP"})

	var body listJobsResponse
	getJSON(t, ts.URL+"/api/jobs?stack=Go", &body)

	if len(body.Jobs) != 1 || body.Jobs[0].Title != "Go Dev" {
		t.Errorf("Jobs = %+v, want just Go Dev", body.Jobs)
	}
}

func TestHandleListJobs_InvalidModeReturns400(t *testing.T) {
	ts, _, _, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/jobs?mode=not-a-real-mode")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleListJobs_KeysetPaginationRoundTrips(t *testing.T) {
	ts, st, _, _ := newTestServer(t)
	sourceID := mustSourceID(t, st, "kalibrr")

	for i := 0; i < 5; i++ {
		seedJob(t, st, sourceID, "Acme", "Job "+string(rune('A'+i)), "https://kalibrr.com/jobs/page-"+string(rune('A'+i)))
	}

	seen := map[string]bool{}
	url := ts.URL + "/api/jobs?limit=2"
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("too many pages — pagination not terminating")
		}

		var body listJobsResponse
		getJSON(t, url, &body)

		for _, j := range body.Jobs {
			if seen[j.ID] {
				t.Fatalf("job %s returned twice across pages", j.ID)
			}
			seen[j.ID] = true
		}

		if body.NextCursor == nil {
			break
		}
		url = ts.URL + "/api/jobs?limit=2&cursor=" + *body.NextCursor
	}

	if len(seen) != 5 {
		t.Errorf("total unique jobs walked = %d, want 5", len(seen))
	}
}

func TestHandleGetJob_FoundWithDuplicates(t *testing.T) {
	ts, st, _, _ := newTestServer(t)
	sourceID := mustSourceID(t, st, "kalibrr")

	canonical := seedJobWithFingerprint(t, st, sourceID, "Acme", "Backend Engineer", "https://kalibrr.com/jobs/canonical", "shared-fp")
	dup := seedJobWithFingerprint(t, st, sourceID, "Acme", "Backend Engineer", "https://kalibrr.com/jobs/dup", "shared-fp")
	_ = dup

	var body jobDetailResponse
	resp, err := http.Get(ts.URL + "/api/jobs/" + canonical.Job.ID.String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Job.ID != canonical.Job.ID.String() {
		t.Errorf("Job.ID = %q, want %q", body.Job.ID, canonical.Job.ID)
	}
	if len(body.Duplicates) != 1 {
		t.Fatalf("len(Duplicates) = %d, want 1", len(body.Duplicates))
	}
	if body.Duplicates[0].SourceURL != "https://kalibrr.com/jobs/dup" {
		t.Errorf("Duplicates[0].SourceURL = %q, want the dup job's URL", body.Duplicates[0].SourceURL)
	}
}

func TestHandleGetJob_NotFoundReturns404(t *testing.T) {
	ts, _, _, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/jobs/" + mustRandomUUID(t).String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleGetJob_InvalidIDReturns400(t *testing.T) {
	ts, _, _, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/jobs/not-a-uuid")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
