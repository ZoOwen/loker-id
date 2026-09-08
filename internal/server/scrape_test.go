package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

	waitForJob(t, pool, "https://kalibrr.com/jobs/triggered")
}

func TestHandleTriggerScrape_InvalidPagesReturns400(t *testing.T) {
	cases := []string{"0", "-1", "21", "abc", "3.5", ""}

	for _, pages := range cases {
		t.Run(pages, func(t *testing.T) {
			ts, _, _, _ := newTestServer(t)
			defer ts.Close()

			url := ts.URL + "/internal/scrape?source=kalibrr&pages=" + pages
			resp := postWithToken(t, url, testInternalToken)
			defer resp.Body.Close()

			// pages="" (the empty string, as opposed to the param being
			// omitted entirely) hits the same "empty value" branch as
			// omitting it, so it's the one case here that's accepted.
			want := http.StatusBadRequest
			if pages == "" {
				want = http.StatusAccepted
			}
			if resp.StatusCode != want {
				t.Errorf("pages=%q: status = %d, want %d", pages, resp.StatusCode, want)
			}
		})
	}
}

func TestHandleTriggerScrape_ValidPagesThreadsThroughToScraper(t *testing.T) {
	ts, _, pool, sc := newTestServer(t)
	defer ts.Close()

	sc.jobs = []scraper.RawJob{
		{Title: "Backend Engineer", Company: "Acme", SourceURL: "https://kalibrr.com/jobs/pages-test", SourceJobID: "pages-test"},
	}

	resp := postWithToken(t, ts.URL+"/internal/scrape?source=kalibrr&pages=7", testInternalToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	waitForJob(t, pool, "https://kalibrr.com/jobs/pages-test")

	if got := sc.gotMaxPages.Load(); got != 7 {
		t.Errorf("scraper received maxPages = %d, want 7", got)
	}
}

func TestHandleTriggerScrape_OmittedPagesLetsScraperApplyItsOwnDefault(t *testing.T) {
	ts, _, pool, sc := newTestServer(t)
	defer ts.Close()

	sc.jobs = []scraper.RawJob{
		{Title: "Backend Engineer", Company: "Acme", SourceURL: "https://kalibrr.com/jobs/no-pages-test", SourceJobID: "no-pages-test"},
	}

	resp := postWithToken(t, ts.URL+"/internal/scrape?source=kalibrr", testInternalToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	waitForJob(t, pool, "https://kalibrr.com/jobs/no-pages-test")

	if got := sc.gotMaxPages.Load(); got != 0 {
		t.Errorf("scraper received maxPages = %d, want 0 (unset, so the scraper applies its own default)", got)
	}
}

// waitForJob polls until a job with the given source_url has landed,
// since the trigger endpoint runs the scrape in a background goroutine.
func waitForJob(t *testing.T, pool *pgxpool.Pool, sourceURL string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := pool.QueryRow(context.Background(),
			"SELECT count(*) FROM jobs WHERE source_url = $1", sourceURL,
		).Scan(&count); err != nil {
			t.Fatalf("count jobs: %v", err)
		}
		if count == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("job %q never persisted within the timeout", sourceURL)
}
