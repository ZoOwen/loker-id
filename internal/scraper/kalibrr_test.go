package scraper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testConfig returns a kalibrrConfig pointed at server with delays and
// retry backoff shortened so tests run in milliseconds, not seconds.
func testConfig(server *httptest.Server) kalibrrConfig {
	cfg := defaultKalibrrConfig()
	cfg.baseURL = server.URL
	// Single keyword by default: these tests are about per-keyword
	// pagination behavior (maxPages, early-stop, retries...), not about
	// aggregating across keywords — that's covered separately by the
	// TestKalibrrScraper_*Keyword* tests, which override this themselves.
	cfg.keywords = []string{"test-keyword"}
	cfg.requestDelay = 0
	cfg.randomDelay = 0
	cfg.requestTimeout = 5 * time.Second
	cfg.retryBaseDelay = 5 * time.Millisecond
	cfg.maxRetries = 3
	cfg.logger = testLogger()
	return cfg
}

func nextDataPage(count int, jobsJSON string) string {
	return fmt.Sprintf(`<html><body><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"count":%d,"jobs":[%s]}}}</script></body></html>`, count, jobsJSON)
}

// sampleJobJSON builds one job's __NEXT_DATA__ JSON with the given id —
// a function rather than a fixed constant so multi-page tests can give
// each page distinct (or, where a test wants it, deliberately identical)
// job ids, since SourceJobID is derived straight from "id".
func sampleJobJSON(id int) string {
	return fmt.Sprintf(`{
		"id": %d,
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
	}`, id)
}

// sampleJobJSONInCountry is sampleJobJSON with a caller-chosen city and
// country, for tests exercising checkKalibrrCountry — the default
// sampleJobJSON is always Indonesia, which isn't enough on its own to
// test the "flag anything else" path.
func sampleJobJSONInCountry(id int, city, country string) string {
	return fmt.Sprintf(`{
		"id": %d,
		"name": "Backend Engineer",
		"companyName": "PT Contoh",
		"company": {"code": "contoh"},
		"slug": "backend-engineer",
		"description": "<p>desc</p>",
		"qualifications": "<p>quals</p>",
		"salaryShown": false,
		"isHybrid": false,
		"isWorkFromHome": false,
		"createdAt": "2026-01-01T00:00:00.000000+00:00",
		"activationDate": "2026-01-02T03:04:05.123456+00:00",
		"googleLocation": {"addressComponents": {"city": %q, "country": %q}}
	}`, id, city, country)
}

// pageNumberFromPath parses the trailing /job-board/te/<keyword>/<page>
// path segment as an int, for handlers that need to vary their response
// by requested page.
func pageNumberFromPath(path string) int {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}

// keywordFromPath parses the /job-board/te/<keyword>/<page> path's
// keyword segment, for handlers that need to vary their response by
// requested keyword.
func keywordFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// neverEndingServer always returns one non-empty, page-uniquely-IDed job
// with a reported total far larger than any test would actually reach —
// so a test using it is exercising exactly one thing: how many pages
// Scrape actually walks, driven purely by maxPages (not by hitting an
// empty page or the reported total).
func neverEndingServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := pageNumberFromPath(r.URL.Path)
		w.Write([]byte(nextDataPage(1_000_000, sampleJobJSON(page))))
	}))
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// capturingLogger is testLogger, but keeps the output around instead of
// discarding it — for tests that need to assert on a specific log line
// (e.g. checkKalibrrCountry's warning) rather than just "did it crash".
func capturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestKalibrrScraper_Source(t *testing.T) {
	s := NewKalibrrScraper()
	if got := s.Source(); got != "kalibrr" {
		t.Errorf("Source() = %q, want %q", got, "kalibrr")
	}
}

func TestKalibrrScraper_NoKeywordsDefaultsToDefaultKalibrrKeywords(t *testing.T) {
	s := NewKalibrrScraper()
	if len(s.keywords) != len(DefaultKalibrrKeywords) {
		t.Fatalf("len(keywords) = %d, want %d (DefaultKalibrrKeywords)", len(s.keywords), len(DefaultKalibrrKeywords))
	}
	for i, kw := range DefaultKalibrrKeywords {
		if s.keywords[i] != kw {
			t.Errorf("keywords[%d] = %q, want %q", i, s.keywords[i], kw)
		}
	}
}

func TestKalibrrScraper_ExplicitKeywordsOverrideTheDefault(t *testing.T) {
	s := NewKalibrrScraper("rust", "kotlin")
	if len(s.keywords) != 2 || s.keywords[0] != "rust" || s.keywords[1] != "kotlin" {
		t.Errorf("keywords = %v, want [rust kotlin]", s.keywords)
	}
}

// TestKalibrrScraper_RealFixture replays a page captured live from
// kalibrr.com/job-board on 2026-09-08 (see testdata/kalibrr_page1.html) to
// confirm our __NEXT_DATA__ decoding and field mapping actually match the
// real site's shape, not just a hand-written fixture. One page is enough
// to verify that; multi-page walking behavior (aggregation, maxPages,
// stopping early) is covered separately with fixtures this test can
// control precisely — this fixture always contains the same 15 jobs no
// matter which page is requested, which would otherwise trip the
// "page duplicates the previous one" early-stop after page 1.
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
	jobs, err := s.Scrape(context.Background(), 1)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}

	if got, want := len(jobs), 15; got != want {
		t.Errorf("len(jobs) = %d, want %d (one page)", got, want)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("requests = %d, want 1", got)
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
	jobs, err := s.Scrape(context.Background(), 3)

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
		w.Write([]byte(nextDataPage(1, sampleJobJSON(999))))
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 3)
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
	_, err := s.Scrape(context.Background(), 3)
	if err == nil {
		t.Fatal("Scrape() error = nil, want an error after exhausting retries")
	}
	if got, want := atomic.LoadInt32(&attempts), int32(cfg.maxRetries); got != want {
		t.Errorf("attempts = %d, want %d (maxRetries)", got, want)
	}
}

// TestKalibrrScraper_StopsOnEmptyPage checks that pagination stops as soon
// as a page comes back with zero jobs, even if the (possibly stale) count
// field would otherwise suggest more pages exist, and well before
// maxPages would have.
func TestKalibrrScraper_StopsOnEmptyPage(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		if n == 1 {
			w.Write([]byte(nextDataPage(999, sampleJobJSON(1))))
			return
		}
		w.Write([]byte(nextDataPage(999, "")))
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 10)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("len(jobs) = %d, want 1", len(jobs))
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("requests = %d, want 2 (page 1 with a job, page 2 empty) — must not keep going to maxPages", got)
	}
}

// TestKalibrrScraper_ContextCancellation checks that an already-canceled
// context stops the scrape immediately instead of making any request.
func TestKalibrrScraper_ContextCancellation(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Write([]byte(nextDataPage(1, sampleJobJSON(999))))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := newKalibrrScraper(testConfig(server))
	_, err := s.Scrape(ctx, 3)
	if err == nil {
		t.Fatal("Scrape() error = nil, want context canceled error")
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Errorf("requests = %d, want 0 (canceled before any request)", got)
	}
}

func TestKalibrrScraper_MultiplePagesAggregate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := pageNumberFromPath(r.URL.Path)
		switch page {
		case 1, 2:
			// Distinct ids per page: two different real jobs, not the
			// same one repeated — a repeated id would (correctly) trip
			// the "page duplicates the previous one" early-stop this
			// same file tests separately below.
			w.Write([]byte(nextDataPage(2, sampleJobJSON(page))))
		default:
			w.Write([]byte(nextDataPage(2, "")))
		}
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 10)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("len(jobs) = %d, want 2 (stops once accumulated >= count)", len(jobs))
	}
}

// TestKalibrrScraper_MaxPagesZeroDefaultsToThree checks that Scrape
// applies scraper.DefaultMaxPages when called with maxPages <= 0.
func TestKalibrrScraper_MaxPagesZeroDefaultsToThree(t *testing.T) {
	server := neverEndingServer()
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 0)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != DefaultMaxPages {
		t.Errorf("len(jobs) = %d, want %d (one job per page, DefaultMaxPages pages)", len(jobs), DefaultMaxPages)
	}
}

// TestKalibrrScraper_MaxPagesRespected checks that an explicit in-range
// maxPages is honored exactly — neither more pages nor fewer.
func TestKalibrrScraper_MaxPagesRespected(t *testing.T) {
	server := neverEndingServer()
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 5)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != 5 {
		t.Errorf("len(jobs) = %d, want 5", len(jobs))
	}
}

// TestKalibrrScraper_MaxPagesCappedAt20 checks that a maxPages far above
// the hard cap is clamped down to it — the server here would happily keep
// paginating forever, so if this didn't clamp, the test would hang making
// hundreds of requests instead of stopping at 20.
func TestKalibrrScraper_MaxPagesCappedAt20(t *testing.T) {
	server := neverEndingServer()
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 500)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != MaxPagesCap {
		t.Errorf("len(jobs) = %d, want %d (MaxPagesCap, not the requested 500)", len(jobs), MaxPagesCap)
	}
}

// TestKalibrrScraper_StopsWhenPageExactlyDuplicatesPrevious checks the
// "job-nya duplikat semua dari halaman sebelumnya" early-stop: a site
// that just keeps re-serving its last real page instead of returning
// empty once paged past the end shouldn't be walked all the way to
// maxPages.
func TestKalibrrScraper_StopsWhenPageExactlyDuplicatesPrevious(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		page := pageNumberFromPath(r.URL.Path)
		twoJobs := sampleJobJSON(1) + "," + sampleJobJSON(2)
		if page == 1 {
			w.Write([]byte(nextDataPage(999, twoJobs)))
			return
		}
		// Every subsequent page: the exact same two jobs again.
		w.Write([]byte(nextDataPage(999, twoJobs)))
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 10)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("len(jobs) = %d, want 2 (only page 1's jobs — page 2's identical jobs must not be re-appended)", len(jobs))
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("requests = %d, want 2 (page 1, then page 2 to discover it's a duplicate — not all the way to maxPages)", got)
	}
}

// TestKalibrrScraper_ContinuesWhenPageHasAtLeastOneNewJob checks that the
// duplicate-page early-stop only fires when a page's jobs are *all*
// duplicates — a page with even one new job alongside repeats must not be
// mistaken for the end of real pagination.
func TestKalibrrScraper_ContinuesWhenPageHasAtLeastOneNewJob(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		page := pageNumberFromPath(r.URL.Path)
		switch page {
		case 1:
			// count is deliberately far above what actually accumulates
			// (4 raw jobs across pages 1-2, including the repeat) so the
			// existing "reached the reported total" stop can't kick in
			// first — page 3 coming back empty must be what stops this.
			w.Write([]byte(nextDataPage(999, sampleJobJSON(1)+","+sampleJobJSON(2))))
		case 2:
			// job 1 repeated, but job 3 is new — not all-duplicate.
			w.Write([]byte(nextDataPage(999, sampleJobJSON(1)+","+sampleJobJSON(3))))
		default:
			w.Write([]byte(nextDataPage(999, "")))
		}
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	jobs, err := s.Scrape(context.Background(), 10)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}
	if len(jobs) != 4 {
		t.Errorf("len(jobs) = %d, want 4 (2 from page 1 + 2 from page 2, including the repeat — page 2 wasn't all-duplicate so it's not skipped)", len(jobs))
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Errorf("requests = %d, want 3 (page 3 comes back empty, which is what actually stops it)", got)
	}
}

// TestKalibrrScraper_ScrapesEveryKeywordAndAggregates checks that Scrape
// runs a separate search per configured keyword and aggregates every
// keyword's jobs into one result — the mechanism this scraper now relies
// on for coverage instead of deep pagination (see the package doc comment
// in kalibrr.go for why).
func TestKalibrrScraper_ScrapesEveryKeywordAndAggregates(t *testing.T) {
	var mu sync.Mutex
	var requestedKeywords []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyword := keywordFromPath(r.URL.Path)
		mu.Lock()
		requestedKeywords = append(requestedKeywords, keyword)
		mu.Unlock()

		if pageNumberFromPath(r.URL.Path) > 1 {
			w.Write([]byte(nextDataPage(1, "")))
			return
		}
		// One job per keyword, id derived from the keyword so each is
		// distinct and traceable back to its search.
		w.Write([]byte(nextDataPage(1, sampleJobJSON(len(keyword)))))
	}))
	defer server.Close()

	cfg := testConfig(server)
	cfg.keywords = []string{"backend", "golang", "devops"}
	s := newKalibrrScraper(cfg)

	jobs, err := s.Scrape(context.Background(), 3)
	if err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}

	if len(jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3 (one per keyword)", len(jobs))
	}

	mu.Lock()
	defer mu.Unlock()
	for _, kw := range cfg.keywords {
		found := false
		for _, got := range requestedKeywords {
			if got == kw {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("keyword %q was never requested; requested = %v", kw, requestedKeywords)
		}
	}
}

// TestKalibrrScraper_ContinuesToNextKeywordAfterOneFails checks that a
// hard failure on one keyword (e.g. the site's response for that search
// doesn't parse) doesn't abort the whole run — the remaining keywords
// still get searched, and both the partial jobs and the failure are
// reported, matching the rest of this codebase's "one bad thing doesn't
// take down everything" pattern (see Pipeline.Run).
func TestKalibrrScraper_ContinuesToNextKeywordAfterOneFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyword := keywordFromPath(r.URL.Path)
		if keyword == "broken" {
			w.Write([]byte(`<html><body>no next data here</body></html>`))
			return
		}
		if pageNumberFromPath(r.URL.Path) > 1 {
			w.Write([]byte(nextDataPage(1, "")))
			return
		}
		w.Write([]byte(nextDataPage(1, sampleJobJSON(len(keyword)))))
	}))
	defer server.Close()

	cfg := testConfig(server)
	cfg.keywords = []string{"backend", "broken", "golang"}
	s := newKalibrrScraper(cfg)

	jobs, err := s.Scrape(context.Background(), 3)
	if err == nil {
		t.Fatal("Scrape() error = nil, want an error mentioning the broken keyword")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("Scrape() error = %q, want it to mention the failing keyword", err.Error())
	}

	if len(jobs) != 2 {
		t.Errorf("len(jobs) = %d, want 2 (backend and golang still succeeded)", len(jobs))
	}
}

// TestKalibrrScraper_RequestsUseTheIndonesiaLocalePrefix guards the fix
// for the 2026-09-09 production incident (see the package doc comment in
// kalibrr.go): every request must carry the /id-ID locale prefix, which
// is what pins the results to Indonesia regardless of the scraping
// server's own geo-IP.
func TestKalibrrScraper_RequestsUseTheIndonesiaLocalePrefix(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(nextDataPage(1, sampleJobJSON(1))))
	}))
	defer server.Close()

	s := newKalibrrScraper(testConfig(server))
	if _, err := s.Scrape(context.Background(), 1); err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}

	if !strings.HasPrefix(gotPath, kalibrrLocalePrefix+"/") {
		t.Errorf("requested path = %q, want it to start with %q", gotPath, kalibrrLocalePrefix+"/")
	}
}

// TestKalibrrScraper_WarnsWhenAJobIsOutsideIndonesia checks
// checkKalibrrCountry's guard: a job with a known, non-Indonesia country
// must produce a warning log naming that country, so a regression of the
// 2026-09-09 incident (locale lock stops working, geoIP takes over again)
// is visible immediately instead of silently landing in the database.
func TestKalibrrScraper_WarnsWhenAJobIsOutsideIndonesia(t *testing.T) {
	jobsJSON := sampleJobJSONInCountry(1, "Central Jakarta", "Indonesia") + "," +
		sampleJobJSONInCountry(2, "Makati", "Philippines")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(nextDataPage(2, jobsJSON)))
	}))
	defer server.Close()

	logger, logs := capturingLogger()
	cfg := testConfig(server)
	cfg.logger = logger
	s := newKalibrrScraper(cfg)

	if _, err := s.Scrape(context.Background(), 1); err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}

	got := logs.String()
	if !strings.Contains(got, "outside the expected country") {
		t.Errorf("logs = %q, want a warning about a job outside the expected country", got)
	}
	if !strings.Contains(got, "Philippines") {
		t.Errorf("logs = %q, want the offending country named", got)
	}
}

// TestKalibrrScraper_NoWarningWhenEveryJobIsIndonesia checks the other
// side of checkKalibrrCountry: an all-Indonesia page (the expected,
// working case) must not produce a warning — otherwise the signal is
// noise and gets ignored exactly when a real incident needs it noticed.
func TestKalibrrScraper_NoWarningWhenEveryJobIsIndonesia(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(nextDataPage(1, sampleJobJSON(1))))
	}))
	defer server.Close()

	logger, logs := capturingLogger()
	cfg := testConfig(server)
	cfg.logger = logger
	s := newKalibrrScraper(cfg)

	if _, err := s.Scrape(context.Background(), 1); err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}

	if got := logs.String(); strings.Contains(got, "outside the expected country") {
		t.Errorf("logs = %q, want no country warning for an all-Indonesia page", got)
	}
}

// TestKalibrrScraper_NoWarningWhenLocationIsUnknown checks that a job
// with no googleLocation at all (Kalibrr doesn't always have geocoded
// location data) is skipped by checkKalibrrCountry rather than flagged —
// "unknown" isn't evidence the locale lock failed.
func TestKalibrrScraper_NoWarningWhenLocationIsUnknown(t *testing.T) {
	jobJSON := `{
		"id": 1, "name": "Backend Engineer", "companyName": "PT Contoh",
		"company": {"code": "contoh"}, "slug": "backend-engineer",
		"description": "", "qualifications": "", "salaryShown": false,
		"isHybrid": false, "isWorkFromHome": false,
		"createdAt": "2026-01-01T00:00:00.000000+00:00",
		"activationDate": "2026-01-02T03:04:05.123456+00:00",
		"googleLocation": null
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(nextDataPage(1, jobJSON)))
	}))
	defer server.Close()

	logger, logs := capturingLogger()
	cfg := testConfig(server)
	cfg.logger = logger
	s := newKalibrrScraper(cfg)

	if _, err := s.Scrape(context.Background(), 1); err != nil {
		t.Fatalf("Scrape() error = %v", err)
	}

	if got := logs.String(); strings.Contains(got, "outside the expected country") {
		t.Errorf("logs = %q, want no country warning when location is unknown", got)
	}
}
