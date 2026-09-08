package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

// Kalibrr renders its job board server-side with Next.js: every listing
// page embeds a `<script id="__NEXT_DATA__">` tag containing the full job
// payload as JSON — the exact data the page hydrates from, not just what's
// visible in the markup. That's far more stable than the page's own CSS
// (Tailwind utility classes plus a hashed emotion/styled-components
// suffix), so this scraper selects that one script tag and decodes its
// JSON instead of scraping job cards. Confirmed by hand against
// kalibrr.com/job-board on 2026-09-08 — no JS execution required, plain
// colly + goquery is enough.
//
// The search URL itself is unusual too: /job-board/{keyword}/{page} does
// NOT filter results (the keyword segment is ignored), but
// /job-board/te/{keyword}/{page} does — "te" is a required, otherwise
// unexplained path segment that switches the router into keyword-search
// mode. Reverse-engineered by probing the live site; not documented
// anywhere public that we found.
const (
	kalibrrSource = "kalibrr"
	kalibrrHost   = "www.kalibrr.com"
	kalibrrBase   = "https://" + kalibrrHost

	// kalibrrUserAgent identifies this bot with a contact point, as
	// opposed to pretending to be a browser.
	kalibrrUserAgent = "loker-id-bot/1.0 (+https://github.com/ZoOwen/loker-id)"

	defaultKalibrrKeyword = "software-engineer"

	kalibrrRequestTimeout = 15 * time.Second
	kalibrrRequestDelay   = 2 * time.Second
	kalibrrRandomDelay    = 1 * time.Second

	kalibrrMaxRetries     = 4
	kalibrrRetryBaseDelay = 1 * time.Second
)

// KalibrrScraper implements Scraper for kalibrr.com job listings matching
// a search keyword.
type KalibrrScraper struct {
	collector      *colly.Collector
	baseURL        string
	keyword        string
	maxRetries     int
	retryBaseDelay time.Duration
	logger         *slog.Logger

	mu     sync.Mutex
	result kalibrrPageResult
}

// kalibrrConfig holds everything newKalibrrScraper needs. It exists
// separately from KalibrrScraper's exported constructor so tests can point
// the scraper at a local fixture server with short retry delays, without
// widening the public API.
type kalibrrConfig struct {
	baseURL        string
	keyword        string
	requestTimeout time.Duration
	requestDelay   time.Duration
	randomDelay    time.Duration
	maxRetries     int
	retryBaseDelay time.Duration
	logger         *slog.Logger
}

func defaultKalibrrConfig() kalibrrConfig {
	return kalibrrConfig{
		baseURL:        kalibrrBase,
		keyword:        defaultKalibrrKeyword,
		requestTimeout: kalibrrRequestTimeout,
		requestDelay:   kalibrrRequestDelay,
		randomDelay:    kalibrrRandomDelay,
		maxRetries:     kalibrrMaxRetries,
		retryBaseDelay: kalibrrRetryBaseDelay,
		logger:         slog.Default(),
	}
}

// kalibrrPageResult carries the outcome of a single page fetch from the
// colly callbacks (which run synchronously inside Visit) back out to the
// calling goroutine.
type kalibrrPageResult struct {
	found     bool // the __NEXT_DATA__ script was located and parsed
	jobs      []RawJob
	count     int
	err       error
	retryable bool
}

// NewKalibrrScraper builds a Scraper for kalibrr.com, searching listings
// that match keyword (e.g. "software-engineer", "backend", "devops"). An
// empty keyword falls back to "software-engineer".
func NewKalibrrScraper(keyword string) *KalibrrScraper {
	cfg := defaultKalibrrConfig()
	if keyword != "" {
		cfg.keyword = keyword
	}
	return newKalibrrScraper(cfg)
}

func newKalibrrScraper(cfg kalibrrConfig) *KalibrrScraper {
	logger := cfg.logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &KalibrrScraper{
		baseURL:        cfg.baseURL,
		keyword:        cfg.keyword,
		maxRetries:     cfg.maxRetries,
		retryBaseDelay: cfg.retryBaseDelay,
		logger:         logger,
	}

	// AllowedDomains matches the request's hostname (no port); LimitRule's
	// DomainGlob matches the full Host (with port). Identical for the real
	// site (no port in the URL), but they diverge for the httptest servers
	// tests point this at, so both must be computed.
	hostname, hostWithPort := cfg.baseURL, cfg.baseURL
	if u, err := url.Parse(cfg.baseURL); err == nil && u.Host != "" {
		hostname, hostWithPort = u.Hostname(), u.Host
	}

	c := colly.NewCollector(
		colly.UserAgent(kalibrrUserAgent),
		colly.AllowedDomains(hostname),
	)
	c.SetClient(&http.Client{Timeout: cfg.requestTimeout})
	// Retries in fetchPage re-visit the same URL after a 429/5xx; colly
	// dedupes visited URLs by default, so that must be disabled.
	c.AllowURLRevisit = true

	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  hostWithPort,
		Delay:       cfg.requestDelay,
		RandomDelay: cfg.randomDelay,
		Parallelism: 1,
	})

	c.OnHTML("script#__NEXT_DATA__", s.handleNextData)
	c.OnError(s.handleError)

	s.collector = c
	return s
}

func (s *KalibrrScraper) Source() string { return kalibrrSource }

func (s *KalibrrScraper) Scrape(ctx context.Context, maxPages int) ([]RawJob, error) {
	maxPages = ClampMaxPages(maxPages)

	var jobs []RawJob
	total := -1
	var prevPageIDs map[string]bool

	for page := 1; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return jobs, fmt.Errorf("scraper: kalibrr: %w", err)
		}

		start := time.Now()
		pageJobs, count, err := s.fetchPage(ctx, page)
		duration := time.Since(start)
		if err != nil {
			return jobs, fmt.Errorf("scraper: kalibrr: page %d: %w", page, err)
		}

		s.logger.Info("scraped page",
			"source", kalibrrSource,
			"page", page,
			"jobs_found", len(pageJobs),
			"duration", duration,
		)

		if len(pageJobs) == 0 {
			s.logger.Info("stopping early: empty page", "source", kalibrrSource, "page", page)
			break
		}

		// Some sites just keep re-serving the last real page instead of
		// returning empty once a caller pages past the end. If every job
		// on this page already appeared on the previous one, there's
		// nothing new here — stop rather than walking all the way to
		// maxPages for no reason.
		pageIDs := kalibrrJobIDSet(pageJobs)
		if prevPageIDs != nil && kalibrrIsSubsetOf(pageIDs, prevPageIDs) {
			s.logger.Info("stopping early: page duplicates the previous one", "source", kalibrrSource, "page", page)
			break
		}
		prevPageIDs = pageIDs

		jobs = append(jobs, pageJobs...)
		total = count
		if total >= 0 && len(jobs) >= total {
			break
		}
	}

	return jobs, nil
}

func kalibrrJobIDSet(jobs []RawJob) map[string]bool {
	set := make(map[string]bool, len(jobs))
	for _, j := range jobs {
		set[j.SourceJobID] = true
	}
	return set
}

// kalibrrIsSubsetOf reports whether every id in a also appears in b — used
// to detect "this page's jobs are all duplicates of the previous page",
// which doesn't require the two pages to be exactly identical (the site
// could return fewer jobs on a trailing page, all of which were already
// seen).
func kalibrrIsSubsetOf(a, b map[string]bool) bool {
	for id := range a {
		if !b[id] {
			return false
		}
	}
	return true
}

// fetchPage visits one listing page, retrying on 429/5xx with exponential
// backoff. A missing __NEXT_DATA__ script (site structure changed, or the
// content turned out to need JS after all) is treated as a hard,
// non-retryable error rather than an empty page, so callers never mistake
// "something broke" for "no more results".
func (s *KalibrrScraper) fetchPage(ctx context.Context, page int) ([]RawJob, int, error) {
	u := fmt.Sprintf("%s/job-board/te/%s/%d", s.baseURL, url.PathEscape(s.keyword), page)

	var res kalibrrPageResult
	err := withBackoff(ctx, s.maxRetries, s.retryBaseDelay, func() (bool, error) {
		s.mu.Lock()
		s.result = kalibrrPageResult{}
		s.mu.Unlock()

		visitErr := s.collector.Visit(u)

		s.mu.Lock()
		res = s.result
		s.mu.Unlock()

		// OnError (which populates res.err/res.retryable with an accurate
		// status-based classification) fires before Visit returns, so it
		// takes priority: Visit's own return value is just that same error
		// bubbled up, except when Visit fails before getting a response at
		// all (bad URL, robots.txt block, domain filter, ...) — those are
		// never retryable.
		if res.err != nil {
			return res.retryable, res.err
		}
		if visitErr != nil {
			return false, fmt.Errorf("visit %s: %w", u, visitErr)
		}
		if !res.found {
			return false, fmt.Errorf("__NEXT_DATA__ not found at %s (page structure changed, or content is client-rendered)", u)
		}
		return false, nil
	})
	if err != nil {
		return nil, 0, err
	}

	return res.jobs, res.count, nil
}

func (s *KalibrrScraper) handleNextData(e *colly.HTMLElement) {
	var payload kalibrrNextData
	if err := json.Unmarshal([]byte(e.Text), &payload); err != nil {
		s.mu.Lock()
		s.result = kalibrrPageResult{err: fmt.Errorf("parse __NEXT_DATA__: %w", err)}
		s.mu.Unlock()
		return
	}

	rawJobs := payload.Props.PageProps.Jobs
	jobs := make([]RawJob, 0, len(rawJobs))
	for _, j := range rawJobs {
		jobs = append(jobs, j.toRawJob(s.baseURL))
	}

	s.mu.Lock()
	s.result = kalibrrPageResult{
		found: true,
		jobs:  jobs,
		count: payload.Props.PageProps.Count,
	}
	s.mu.Unlock()
}

func (s *KalibrrScraper) handleError(r *colly.Response, err error) {
	status := 0
	if r != nil {
		status = r.StatusCode
	}

	s.mu.Lock()
	s.result = kalibrrPageResult{
		err:       fmt.Errorf("http %d: %w", status, err),
		retryable: status == http.StatusTooManyRequests || status >= 500,
	}
	s.mu.Unlock()
}

// kalibrrNextData is the subset of Kalibrr's __NEXT_DATA__ payload we
// need. Unknown fields are ignored by encoding/json, so this only lists
// what's actually used.
type kalibrrNextData struct {
	Props struct {
		PageProps struct {
			Count int          `json:"count"`
			Jobs  []kalibrrJob `json:"jobs"`
		} `json:"pageProps"`
	} `json:"props"`
}

type kalibrrJob struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	CompanyName string `json:"companyName"`
	Company     struct {
		Code string `json:"code"`
	} `json:"company"`
	Slug           string  `json:"slug"`
	Description    string  `json:"description"`
	Qualifications string  `json:"qualifications"`
	BaseSalary     *int64  `json:"baseSalary"`
	MaximumSalary  *int64  `json:"maximumSalary"`
	SalaryCurrency *string `json:"salaryCurrency"`
	SalaryInterval *string `json:"salaryInterval"`
	SalaryShown    bool    `json:"salaryShown"`
	IsHybrid       bool    `json:"isHybrid"`
	IsWorkFromHome bool    `json:"isWorkFromHome"`
	CreatedAt      string  `json:"createdAt"`
	ActivationDate string  `json:"activationDate"`
	GoogleLocation *struct {
		AddressComponents struct {
			City    string `json:"city"`
			Country string `json:"country"`
		} `json:"addressComponents"`
	} `json:"googleLocation"`
}

func (j kalibrrJob) toRawJob(baseURL string) RawJob {
	rj := RawJob{
		Title:       j.Name,
		Company:     j.CompanyName,
		SalaryRaw:   j.salaryRawText(),
		Location:    j.locationText(),
		Description: strings.TrimSpace(j.Description + "\n\n" + j.Qualifications),
		SourceURL:   fmt.Sprintf("%s/c/%s/jobs/%d/%s", baseURL, j.Company.Code, j.ID, j.Slug),
		SourceJobID: strconv.FormatInt(j.ID, 10),
	}

	// Kalibrr's own timestamps carry microsecond precision (e.g.
	// "...165396+00:00"); RFC3339Nano is RFC3339 that also accepts that.
	if t, err := time.Parse(time.RFC3339Nano, j.ActivationDate); err == nil {
		rj.PostedAt = &t
	}

	return rj
}

// salaryRawText mirrors what Kalibrr itself would show: nothing when the
// listing hides its salary (salaryShown is a distinct flag from whether
// the API happens to return figures — some jobs carry salary data the UI
// never displays), otherwise a plain "<currency> <min> - <max>/<interval>"
// string for the parser layer to normalize later.
func (j kalibrrJob) salaryRawText() string {
	if !j.SalaryShown || (j.BaseSalary == nil && j.MaximumSalary == nil) {
		return ""
	}

	currency := "IDR"
	if j.SalaryCurrency != nil && *j.SalaryCurrency != "" {
		currency = *j.SalaryCurrency
	}

	interval := ""
	if j.SalaryInterval != nil && *j.SalaryInterval != "" {
		interval = "/" + *j.SalaryInterval
	}

	switch {
	case j.BaseSalary != nil && j.MaximumSalary != nil:
		return fmt.Sprintf("%s %d - %d%s", currency, *j.BaseSalary, *j.MaximumSalary, interval)
	case j.MaximumSalary != nil:
		return fmt.Sprintf("up to %s %d%s", currency, *j.MaximumSalary, interval)
	case j.BaseSalary != nil:
		return fmt.Sprintf("from %s %d%s", currency, *j.BaseSalary, interval)
	default:
		return ""
	}
}

func (j kalibrrJob) locationText() string {
	if j.GoogleLocation == nil {
		return ""
	}

	city := j.GoogleLocation.AddressComponents.City
	country := j.GoogleLocation.AddressComponents.Country

	var loc string
	switch {
	case city != "" && country != "":
		loc = city + ", " + country
	case city != "":
		loc = city
	default:
		loc = country
	}

	switch {
	case j.IsWorkFromHome:
		loc += " (Remote)"
	case j.IsHybrid:
		loc += " (Hybrid)"
	}

	return loc
}
