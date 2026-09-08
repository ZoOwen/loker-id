package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ZoOwen/loker-id/internal/scraper"
)

func postWithToken(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestHandleTriggerScrape_MissingTokenReturns401(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	defer ts.Close()

	resp := postWithToken(t, ts.URL+"/internal/scrape?source=kalibrr", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandleTriggerScrape_WrongTokenReturns401(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	defer ts.Close()

	resp := postWithToken(t, ts.URL+"/internal/scrape?source=kalibrr", "wrong-token")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandleTriggerScrape_MissingSourceReturns400(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	defer ts.Close()

	resp := postWithToken(t, ts.URL+"/internal/scrape", testInternalToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTriggerScrape_UnknownSourceReturns400(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	defer ts.Close()

	resp := postWithToken(t, ts.URL+"/internal/scrape?source=does-not-exist", testInternalToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTriggerScrape_ValidRequestReturns202AndRunsInBackground(t *testing.T) {
	ts, _, pool, sc := newTestServer(t)
	defer ts.Close()

	sc.jobs = []scraper.RawJob{
		{Title: "Backend Engineer", Company: "Acme", SourceURL: "https://kalibrr.com/jobs/triggered", SourceJobID: "1"},
	}

	start := time.Now()
	resp := postWithToken(t, ts.URL+"/internal/scrape?source=kalibrr", testInternalToken)
	elapsed := time.Since(start)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("POST took %v, want it to return immediately (work happens in a goroutine)", elapsed)
	}

	// The run happens in the background; poll briefly for it to land
	// rather than assuming a fixed sleep is enough.
	deadline := time.Now().Add(5 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		var count int
		if err := pool.QueryRow(context.Background(),
			"SELECT count(*) FROM jobs WHERE source_url = $1", "https://kalibrr.com/jobs/triggered",
		).Scan(&count); err != nil {
			t.Fatalf("count jobs: %v", err)
		}
		if count == 1 {
			found = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !found {
		t.Fatal("triggered scrape never persisted the job within the timeout")
	}
}
