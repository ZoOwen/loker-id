package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testConfig returns a kalibrrConfig pointed at server with delays and
// retry backoff shortened so tests run in milliseconds, not seconds.
func testConfig(server *httptest.Server) kalibrrConfig {
	cfg := defaultKalibrrConfig()
	cfg.baseURL = server.URL
	cfg.requestDelay = 0
	cfg.randomDelay = 0
	cfg.requestTimeout = 5 * time.Second
	cfg.retryBaseDelay = 5 * time.Millisecond
	cfg.maxRetries = 3
	return cfg
}

func nextDataPage(count int, jobsJSON string) string {
	return fmt.Sprintf(`<html><body><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"count":%d,"jobs":[%s]}}}</script></body></html>`, count, jobsJSON)
}

const sampleJobJSON = `{
	"id": 999,
	"name": "Backend Engineer",
	"companyName": "PT Contoh",
	"company": {"code": "contoh"},
	"slug": "backend-engineer",
	"description": "<p>desc</p>",
	"qualifications": "<p>quals</p>",
	"baseSalary": 5000000,
	"maximumSalary": 8000000,
	"salaryCurrency": "IDR",
	"salaryInterval": "month",
	"salaryShown": true,
	"isHybrid": true,
	"isWorkFromHome": false,
	"createdAt": "2026-01-01T00:00:00.000000+00:00",
	"activationDate": "2026-01-02T03:04:05.123456+00:00",
	"googleLocation": {"addressComponents": {"city": "Bandung", "country": "Indonesia"}}
}`

func TestKalibrrScraper_Source(t *testing.T) {
	s := NewKalibrrScraper("")
	if got := s.Source(); got != "kalibrr" {
		t.Errorf("Source() = %q, want %q", got, "kalibrr")
	}
}

// TestKalibrrScraper_RealFixture replays a page captured live from
// kalibrr.com/job-board on 2026-09-08 (see testdata/kalibrr_page1.html) to
// confirm our __NEXT_DATA__ decoding and field mapping actually match the
// real site's shape, not just a hand-written fixture. The server ignores
// the requested page and always returns this same 15-job/count-37 page, so
// Scrape should paginate exactly 3 times (15+15+15=45 >= 37) and stop.
func TestKalibrrScraper_RealFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/kalibrr_page1.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(fixture)
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}

	if got, want := len(jobs), 45; got != want {
		t.Errorf("len(jobs) = %d, want %d (3 pages x 15)", got, want)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}

	first := jobs[0]
	if first.Title != "Tech Recruiter" {
		t.Errorf("jobs[0].Title = %q, want %q", first.Title, "Tech Recruiter")
	}
	if first.Company != "PT BFI Finance Indonesia Tbk" {
		t.Errorf("jobs[0].Company = %q, want %q", first.Company, "PT BFI Finance Indonesia Tbk")
	}
	if first.SourceJobID != "268316" {
		t.Errorf("jobs[0].SourceJobID = %q, want %q", first.SourceJobID, "268316")
	}
	if !strings.HasSuffix(first.SourceURL, "/c/bfifinance/jobs/268316/tech-recruiter-2") {
		t.Errorf("jobs[0].SourceURL = %q, want suffix %q", first.SourceURL, "/c/bfifinance/jobs/268316/tech-recruiter-2")
	}
	if first.Location != "Tangerang, Indonesia" {
		t.Errorf("jobs[0].Location = %q, want %q", first.Location, "Tangerang, Indonesia")
	}
	if first.PostedAt == nil || first.PostedAt.Format(time.RFC3339) != "2026-06-09T07:30:24Z" {
		t.Errorf("jobs[0].PostedAt = %v, want 2026-06-09T07:30:24Z", first.PostedAt)
	}
	// This job has baseSalary/maximumSalary null in the real payload, so
	// salaryShown alone isn't enough to produce a raw string.
	if first.SalaryRaw != "" {
		t.Errorf("jobs[0].SalaryRaw = %q, want empty (no salary figures on this job)", first.SalaryRaw)
	}

	// job id 271601 in the fixture has real salary figures shown.
	var withSalary *RawJob
	for i := range jobs {
		if jobs[i].SourceJobID == "271601" {
			withSalary = &jobs[i]
			break
		}
	}
	if withSalary == nil {
		t.Fatal("expected job 271601 (has salary figures) in fixture, not found")
	}
	if withSalary.SalaryRaw != "IDR 4600000 - 4700000/month" {
		t.Errorf("job 271601 SalaryRaw = %q, want %q", withSalary.SalaryRaw, "IDR 4600000 - 4700000/month")
	}
}

// TestKalibrrScraper_MissingNextData ensures that a page which doesn't
// contain the __NEXT_DATA__ script (site restructured, or turned out to
// need JS) surfaces as a clear error instead of silently looking like "no
// more jobs".
func TestKalibrrScraper_MissingNextData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="app">rendered client-side</div></body></html>`))
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background())

	if err == nil {
		t.Fatal("Scrape() error = nil, want an error about missing __NEXT_DATA__")
	}
	if !strings.Contains(err.Error(), "__NEXT_DATA__") {
		t.Errorf("Scrape() error = %q, want it to mention __NEXT_DATA__", err.Error())
	}
	if len(jobs) != 0 {
		t.Errorf("len(jobs) = %d, want 0", len(jobs))
	}
}

// TestKalibrrScraper_RetriesOn429 checks that a transient 429 is retried
// (with backoff) and a subsequent success is used, rather than failing the
// whole scrape immediately.
func TestKalibrrScraper_RetriesOn429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(nextDataPage(1, sampleJobJSON)))
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if got, want := atomic.LoadInt32(&attempts), int32(3); got != want {
		t.Errorf("attempts = %d, want %d (2 failures + 1 success)", got, want)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].Title != "Backend Engineer" {
		t.Errorf("jobs[0].Title = %q, want %q", jobs[0].Title, "Backend Engineer")
	}
}

// TestKalibrrScraper_GivesUpAfterMaxRetries checks that a persistent 5xx
// is eventually reported as an error rather than retried forever.
func TestKalibrrScraper_GivesUpAfterMaxRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testConfig(server)
	s := newKalibrrScraper(cfg)
	_, err := s.Scrape(context.Background())
	if err == nil {
		t.Fatal("Scrape() error = nil, want an error after exhausting retries")
	}
	if got, want := atomic.LoadInt32(&attempts), int32(cfg.maxRetries); got != want {
		t.Errorf("attempts = %d, want %d (maxRetries)", got, want)
	}
}

// TestKalibrrScraper_StopsOnEmptyPage checks that pagination stops as soon
// as a page comes back with zero jobs, even if the (possibly stale) count
// field would otherwise suggest more pages exist.
func TestKalibrrScraper_StopsOnEmptyPage(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		if n == 1 {
			w.Write([]byte(nextDataPage(999, sampleJobJSON)))
			return
		}
		w.Write([]byte(nextDataPage(999, "")))
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("len(jobs) = %d, want 1", len(jobs))
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("requests = %d, want 2 (page 1 with a job, page 2 empty)", got)
	}
}

// TestKalibrrScraper_ContextCancellation checks that an already-canceled
// context stops the scrape immediately instead of making any request.
func TestKalibrrScraper_ContextCancellation(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Write([]byte(nextDataPage(1, sampleJobJSON)))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := newKalibrrScraper(testConfig(server))
	_, err := s.Scrape(ctx)
	if err == nil {
		t.Fatal("Scrape() error = nil, want context canceled error")
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Errorf("requests = %d, want 0 (canceled before any request)", got)
	}
}

func TestKalibrrScraper_MultiplePagesAggregate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /job-board/te/<keyword>/<page>
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		page, _ := strconv.Atoi(parts[len(parts)-1])

		switch page {
		case 1, 2:
			w.Write([]byte(nextDataPage(2, sampleJobJSON)))
		default:
			w.Write([]byte(nextDataPage(2, "")))
		}
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("len(jobs) = %d, want 2 (stops once accumulated >= count)", len(jobs))
	}
}
