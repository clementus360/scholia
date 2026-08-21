package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// article is one encyclopedia entry, quoted rather than summarised.
//
// Extract is the lead paragraph exactly as Wikipedia serves it. Nothing in this
// pipeline rewrites it, and nothing written by a model is stored in it — the
// model's only role is deciding which article to look up. Revision is what
// makes the quotation checkable: it pins the text to a specific version of the
// page, so a reader following the link years later can see what changed.
type article struct {
	ID          string `json:"id"`
	WikidataID  string `json:"wikidata_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Extract     string `json:"extract"`
	URL         string `json:"url"`
	Revision    int64  `json:"revision"`
	Retrieved   string `json:"retrieved"`
	License     string `json:"license"`
}

// wikiClient talks to Wikidata and Wikipedia, with a shared cache so the many
// events that mention Sennacherib fetch his article once.
//
// Wikimedia asks API clients to identify themselves and to keep concurrency
// modest; the user agent and the semaphore below are that courtesy, not a
// performance choice.
type wikiClient struct {
	http      *http.Client
	userAgent string
	limiter   chan struct{}

	mu    sync.Mutex
	cache map[string]*article
	// missing remembers lookups that failed so a bad term proposed by fifty
	// events costs one round trip rather than fifty.
	missing map[string]bool
}

func newWikiClient(userAgent string, concurrency int) *wikiClient {
	return &wikiClient{
		http:      &http.Client{Timeout: 30 * time.Second},
		userAgent: userAgent,
		limiter:   make(chan struct{}, concurrency),
		cache:     map[string]*article{},
		missing:   map[string]bool{},
	}
}

func (c *wikiClient) get(endpoint string, out any) error {
	c.limiter <- struct{}{}
	defer func() { <-c.limiter }()

	var lastErr error

	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		request, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		request.Header.Set("User-Agent", c.userAgent)
		request.Header.Set("Accept", "application/json")

		response, err := c.http.Do(request)
		if err != nil {
			lastErr = err
			continue
		}

		// A 404 is a real answer — the page does not exist — so it is returned
		// rather than retried.
		if response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			return errNotFound
		}

		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			lastErr = fmt.Errorf("%s: HTTP %d", endpoint, response.StatusCode)
			continue
		}

		err = json.NewDecoder(response.Body).Decode(out)
		response.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	return lastErr
}

var errNotFound = fmt.Errorf("not found")

// resolve turns a proposed search term into a real article, or into nothing.
//
// This is the step that makes the difference between a citation and a guess.
// The model may propose a plausible-sounding title that no encyclopedia has, or
// a real title that means something else entirely — "Ur" is a Sumerian city and
// also a German prefix and a band. Requiring the term to land on a Wikidata
// entity that owns an English Wikipedia page throws both away: an unresolvable
// term yields nil, and a resolvable one yields text nobody in this pipeline
// wrote.
func (c *wikiClient) resolve(term string) (*article, error) {
	key := strings.ToLower(strings.TrimSpace(term))
	if key == "" {
		return nil, nil
	}

	c.mu.Lock()
	if cached, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return cached, nil
	}
	if c.missing[key] {
		c.mu.Unlock()
		return nil, nil
	}
	c.mu.Unlock()

	found, err := c.lookup(term)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if found == nil {
		c.missing[key] = true
		return nil, nil
	}

	// Two terms often resolve to the same page ("Sennacherib" and "King
	// Sennacherib"), so dedupe on the Wikidata id and keep one copy.
	for _, existing := range c.cache {
		if existing.WikidataID == found.WikidataID {
			c.cache[key] = existing
			return existing, nil
		}
	}

	c.cache[key] = found

	return found, nil
}

func (c *wikiClient) lookup(term string) (*article, error) {
	entityID, err := c.searchEntity(term)
	if err != nil || entityID == "" {
		return nil, err
	}

	pageTitle, err := c.englishPage(entityID)
	if err != nil || pageTitle == "" {
		return nil, err
	}

	return c.summary(entityID, pageTitle)
}

// searchEntity asks Wikidata which entity a term names.
func (c *wikiClient) searchEntity(term string) (string, error) {
	endpoint := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action":   {"wbsearchentities"},
		"search":   {term},
		"language": {"en"},
		"uselang":  {"en"},
		"format":   {"json"},
		"limit":    {"1"},
	}.Encode()

	var payload struct {
		Search []struct {
			ID string `json:"id"`
		} `json:"search"`
	}

	if err := c.get(endpoint, &payload); err != nil {
		return "", err
	}

	if len(payload.Search) == 0 {
		return "", nil
	}

	return payload.Search[0].ID, nil
}

// englishPage returns the entity's English Wikipedia title, if it has one.
//
// Entities without an English page are dropped rather than followed to another
// language: the app is English-only, and a citation a reader cannot read is
// worse than no citation.
func (c *wikiClient) englishPage(entityID string) (string, error) {
	endpoint := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action":     {"wbgetentities"},
		"ids":        {entityID},
		"props":      {"sitelinks"},
		"sitefilter": {"enwiki"},
		"format":     {"json"},
	}.Encode()

	var payload struct {
		Entities map[string]struct {
			Sitelinks map[string]struct {
				Title string `json:"title"`
			} `json:"sitelinks"`
		} `json:"entities"`
	}

	if err := c.get(endpoint, &payload); err != nil {
		return "", err
	}

	entity, ok := payload.Entities[entityID]
	if !ok {
		return "", nil
	}

	return entity.Sitelinks["enwiki"].Title, nil
}

// summary fetches the lead extract, its canonical URL and its revision id.
func (c *wikiClient) summary(entityID, pageTitle string) (*article, error) {
	endpoint := "https://en.wikipedia.org/api/rest_v1/page/summary/" + url.PathEscape(strings.ReplaceAll(pageTitle, " ", "_"))

	var payload struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Extract     string `json:"extract"`
		Revision    string `json:"revision"`
		Timestamp   string `json:"timestamp"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}

	if err := c.get(endpoint, &payload); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}

	// "standard" is a normal article. Disambiguation pages and redirects to
	// list pages carry other types and are never worth quoting.
	if payload.Type != "standard" || strings.TrimSpace(payload.Extract) == "" {
		return nil, nil
	}

	pageURL := payload.ContentURLs.Desktop.Page
	if pageURL == "" {
		pageURL = "https://en.wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(payload.Title, " ", "_"))
	}

	return &article{
		ID:          strings.ToLower(entityID),
		WikidataID:  entityID,
		Title:       payload.Title,
		Description: payload.Description,
		Extract:     strings.TrimSpace(payload.Extract),
		URL:         pageURL,
		Revision:    parseRevision(payload.Revision),
		Retrieved:   strings.TrimSpace(payload.Timestamp),
		License:     "cc-by-sa-4",
	}, nil
}

func parseRevision(raw string) int64 {
	var revision int64

	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &revision); err != nil {
		return 0
	}

	return revision
}
