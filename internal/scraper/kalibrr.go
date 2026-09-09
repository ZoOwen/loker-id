package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
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
//
// Coverage comes from the keyword list, not deep pagination, and that's a
// deliberate call, not an oversight: as of 2026-09-09, Kalibrr's own
// server-side pagination for this route is broken. Fetching pages 1, 2,
// and 3 of the same keyword and diffing their embedded __NEXT_DATA__
// shows the page path segment reaching the Next.js router fine
// (query.param's third element is genuinely "1"/"2"/"3"), but
// pageProps.filters.offset comes back as 0 on every single one — the
// page number just isn't translated into a query offset server-side, so
// every page replays the same first `limit` (15) results. Query-string
// variants (?page=2, ?offset=15) don't change that either. Our own
// maxPages/early-stop logic (see Scrape) is correct and does exactly what
// it should here — it detects page 2 duplicating page 1 and stops after
// one page, per keyword. There's simply nothing further to page into
// right now. If Kalibrr ever fixes their pagination, this scraper starts
// walking multiple pages per keyword automatically — nothing here assumes
// it's broken forever, it just isn't relied upon.
//
// So instead of depth (pages), this scraper gets breadth (keywords):
// DefaultKalibrrKeywords runs one search per keyword and aggregates the
// results. The same real-world posting routinely turns up under more than
// one keyword (e.g. a "Backend Engineer (Golang)" role matches both
// "backend" and "golang") — that's expected, and internal/store's
// existing fingerprint/source_url dedup is what collapses it back down to
// one job, not anything special here. See store.UpsertJobResult.WasInserted
// and Pipeline.Run's JobsNew/JobsDuplicate accounting for how that shows
// up in a run's numbers.
//
// The country these searches return is geo-IP-based, not fixed by
// keyword, "te" search mode, or (it turns out) the /id-ID locale prefix
// this package tried first. Two rounds of a real production incident
// (server deployed to Render's Singapore region) established that:
//
//  1. First occurrence (2026-09-09): every scraped job came back
//     Filipino (Makati, Pasig, Quezon City...), zero Indonesian, while
//     the same code run from an Indonesia-resident IP was fine. The fix
//     tried then was prefixing requests with /id-ID (Next.js's built-in
//     locale-routing prefix — kalibrr.com serves exactly two locales,
//     "en" and "id-ID"), verified by fetching that URL and confirming
//     `"locale":"id-ID"` plus Indonesian job locations in the response.
//  2. That "fix" then failed in production anyway: the very same
//     www.kalibrr.com/id-ID/... URL returned wrong_country_jobs=15/15,
//     all Philippines, from Render. The verification behind step 1 was
//     methodologically broken — fetching /id-ID from a dev machine whose
//     own IP already geolocates to Indonesia proves nothing about
//     whether the prefix itself does anything; the locale prefix turned
//     out to only ever have controlled the UI language, never which
//     country's jobs get returned. geoCountry (still keyed off the
//     request's IP) does.
//
// The mechanism that actually is IP-independent, verified with a test
// that doesn't depend on this environment's own geolocation: Kalibrr
// operates separate per-country domains, visible in the page's own CSP
// header (kalibrr.com, kalibrr.id, kalibrr.ph, kalibrr.vn). Fetching
// https://www.kalibrr.ph/job-board/te/backend/1 from this same
// Indonesia-geolocated environment returned geoCountry
// {"country":"PH","actualCountry":"PH"} and 15/15 Philippines jobs —
// i.e. the exact same source IP got opposite-country results purely by
// switching domains. That can only be explained by the domain driving
// the result, not the request's IP, and by symmetry it means
// www.kalibrr.id locks Indonesia the same way regardless of where the
// request originates (Render Singapore included). kalibrr.co.id, tried
// first as the more guessable candidate, turned out to just 308-redirect
// to www.kalibrr.id — that redirect is followed fine (colly's default
// http.Client behavior), but this scraper points at kalibrr.id directly
// rather than relying on it. Re-verified on kalibrr.id that "te" keyword
// search still works (count differs per keyword; a keyword search
// without "te" ignores the keyword, same as on kalibrr.com) and that
// pagination is equally broken there (offset stuck at 0 — see above),
// so nothing else about this scraper's behavior needed to change.
//
// The /id-ID locale prefix is kept anyway — it's harmless, still genuine
// Next.js locale routing, and one live check showed it reduces stray
// non-Indonesia jobs to zero (vs. one leaking through on kalibrr.com/en
// without it) — but it is not what makes this work; the domain is. Given
// this mechanism has already been wrong once, checkKalibrrCountry (called
// from handleNextData) logs a warning if a scrape ever comes back
// non-Indonesian anyway, so a second regression surfaces from production
// logs immediately instead of silently polluting the database again.
const (
	kalibrrSource = "kalibrr"
	// kalibrrHost is Kalibrr's Indonesia-specific domain — see the
	// package doc comment above for why this, and not kalibrr.com, is
	// what actually locks results to Indonesia.
	kalibrrHost = "www.kalibrr.id"
	kalibrrBase = "https://" + kalibrrHost

	// kalibrrLocalePrefix sets Kalibrr's UI locale to Indonesian on top
	// of kalibrrHost already locking the country — see the package doc
	// comment above.
	kalibrrLocalePrefix = "/id-ID"

	// kalibrrExpectedCountry is what every job's
	// googleLocation.addressComponents.country should read once
	// kalibrrLocalePrefix is doing its job — see checkKalibrrCountry.
	kalibrrExpectedCountry = "Indonesia"

	// kalibrrUserAgent identifies this bot with a contact point, as
	// opposed to pretending to be a browser.
	kalibrrUserAgent = "loker-id-bot/1.0 (+https://github.com/ZoOwen/loker-id)"

	kalibrrRequestTimeout = 15 * time.Second
	kalibrrRequestDelay   = 2 * time.Second
	kalibrrRandomDelay    = 1 * time.Second

	kalibrrMaxRetries     = 4
	kalibrrRetryBaseDelay = 1 * time.Second
)

// DefaultKalibrrKeywords is the keyword set NewKalibrrScraper searches by
// default when called with none of its own — chosen to cover the
// practical breadth of Indonesian dev-job listings on Kalibrr, since deep
// pagination isn't an option right now (see the package doc comment
// above).
var DefaultKalibrrKeywords = []string{
	"backend", "frontend", "fullstack", "devops", "mobile",
	"data engineer", "qa", "golang", "react", "python",
	"java", "php", "nodejs", "android", "ios",
}

// KalibrrScraper implements Scraper for Kalibrr's Indonesia job listings
// (kalibrr.id — see the package doc comment for why that domain
// specifically), running one search per keyword and aggregating the
// results (see the package doc comment for why keywords, not pages, are
// how this scraper gets coverage).
type KalibrrScraper struct {
	collector      *colly.Collector
	baseURL        string
	keywords       []string
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
	keywords       []string
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
		keywords:       DefaultKalibrrKeywords,
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

// NewKalibrrScraper builds a Scraper for Kalibrr's Indonesia listings,
// running one search per keyword given (e.g. "backend", "golang",
// "devops") and aggregating the results. Called with no keywords, it
// searches DefaultKalibrrKeywords instead.
func NewKalibrrScraper(keywords ...string) *KalibrrScraper {
	cfg := defaultKalibrrConfig()
	if len(keywords) > 0 {
		cfg.keywords = keywords
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
		keywords:       cfg.keywords,
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

// Scrape runs one paginated search per configured keyword (see
// DefaultKalibrrKeywords and the package doc comment) and aggregates
// every keyword's jobs into one slice. A keyword that comes back with an
// error doesn't stop the others — it's collected and joined into the
// returned error alongside whatever jobs were gathered, the same
// "partial results, not a silent loss" contract Scrape has always had for
// a single search. The same real-world job commonly turns up under more
// than one keyword; that's expected and left for internal/store's
// existing dedup to collapse — see the package doc comment.
func (s *KalibrrScraper) Scrape(ctx context.Context, maxPages int) ([]RawJob, error) {
	maxPages = ClampMaxPages(maxPages)

	var jobs []RawJob
	var errs []error

	for _, keyword := range s.keywords {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}

		keywordJobs, err := s.scrapeKeyword(ctx, keyword, maxPages)
		jobs = append(jobs, keywordJobs...)
		if err != nil {
			errs = append(errs, fmt.Errorf("keyword %q: %w", keyword, err))
		}
	}

	if len(errs) > 0 {
		return jobs, fmt.Errorf("scraper: kalibrr: %w", errors.Join(errs...))
	}
	return jobs, nil
}

// scrapeKeyword walks up to maxPages of results for a single keyword,
// stopping early on an empty page or one whose jobs all duplicate the
// previous page — see Scrape's doc comment on why that's expected to
// trigger after just one page for the time being.
func (s *KalibrrScraper) scrapeKeyword(ctx context.Context, keyword string, maxPages int) ([]RawJob, error) {
	var jobs []RawJob
	total := -1
	var prevPageIDs map[string]bool

	for page := 1; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return jobs, err
		}

		start := time.Now()
		pageJobs, count, err := s.fetchPage(ctx, keyword, page)
		duration := time.Since(start)
		if err != nil {
			return jobs, fmt.Errorf("page %d: %w", page, err)
		}

		s.logger.Info("scraped page",
			"source", kalibrrSource,
			"keyword", keyword,
			"page", page,
			"jobs_found", len(pageJobs),
			"duration", duration,
		)

		if len(pageJobs) == 0 {
			s.logger.Info("stopping early: empty page", "source", kalibrrSource, "keyword", keyword, "page", page)
			break
		}

		// Some sites just keep re-serving the last real page instead of
		// returning empty once a caller pages past the end. If every job
		// on this page already appeared on the previous one, there's
		// nothing new here — stop rather than walking all the way to
		// maxPages for no reason.
		pageIDs := kalibrrJobIDSet(pageJobs)
		if prevPageIDs != nil && kalibrrIsSubsetOf(pageIDs, prevPageIDs) {
			s.logger.Info("stopping early: page duplicates the previous one", "source", kalibrrSource, "keyword", keyword, "page", page)
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
func (s *KalibrrScraper) fetchPage(ctx context.Context, keyword string, page int) ([]RawJob, int, error) {
	u := fmt.Sprintf("%s%s/job-board/te/%s/%d", s.baseURL, kalibrrLocalePrefix, url.PathEscape(keyword), page)

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

	s.checkKalibrrCountry(rawJobs, e.Request.URL.String())

	s.mu.Lock()
	s.result = kalibrrPageResult{
		found: true,
		jobs:  jobs,
		count: payload.Props.PageProps.Count,
	}
	s.mu.Unlock()
}

// checkKalibrrCountry logs a warning if any job on this page carries a
// known country other than kalibrrExpectedCountry. kalibrrLocalePrefix
// should make that impossible (see the package doc comment for the
// 2026-09-09 incident this guards against) — but if Kalibrr's routing or
// geoIP behavior ever changes underneath us, this is what surfaces it
// immediately instead of silently filling the database with the wrong
// country's jobs again. A job with no location data at all is skipped
// rather than flagged — "unknown" isn't evidence of anything wrong.
func (s *KalibrrScraper) checkKalibrrCountry(jobs []kalibrrJob, url string) {
	var wrong int
	seen := make(map[string]bool)
	for _, j := range jobs {
		if j.GoogleLocation == nil {
			continue
		}
		country := j.GoogleLocation.AddressComponents.Country
		if country == "" || country == kalibrrExpectedCountry {
			continue
		}
		wrong++
		seen[country] = true
	}
	if wrong == 0 {
		return
	}

	countries := make([]string, 0, len(seen))
	for c := range seen {
		countries = append(countries, c)
	}
	sort.Strings(countries)

	s.logger.Warn("scraped jobs outside the expected country — locale lock may not be working",
		"source", kalibrrSource,
		"url", url,
		"expected_country", kalibrrExpectedCountry,
		"total_jobs", len(jobs),
		"wrong_country_jobs", wrong,
		"countries_seen", countries,
	)
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
