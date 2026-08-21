package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// The model is used for exactly two jobs, and neither of them writes anything a
// reader sees:
//
//  1. propose — turn a passage into candidate encyclopedia article titles.
//  2. judge   — say whether a fetched article actually bears on the passage.
//
// Both are filters over other people's writing. Everything a reader reads comes
// back verbatim from Wikipedia, so a hallucinated title costs a wasted lookup
// rather than a false statement in the app.

type proposal struct {
	Query string `json:"query"`
	Why   string `json:"why"`
}

type verdict struct {
	// Index points back into the candidate list the prompt numbered.
	Index int  `json:"index"`
	Keep  bool `json:"keep"`
	// Kind is "history" or "parallel". The split exists because the two are
	// different claims: that Nebuchadnezzar II besieged Jerusalem is history of
	// the events, while that Enūma Eliš resembles Genesis 1 is a comparison
	// between texts, and how the two relate is contested. Keeping them in one
	// undifferentiated list would make the app appear to take a position it has
	// no business taking, so the UI renders them apart and says so.
	Kind      string `json:"kind"`
	Relevance string `json:"relevance"`
}

// llm is the provider-independent surface the pipeline uses.
type llm interface {
	// complete sends a prompt plus a JSON schema and returns the raw JSON.
	complete(prompt string, schema map[string]any) ([]byte, error)
	name() string
}

const proposeInstruction = `You are helping a Bible reader understand the external history behind a passage: the empires, rulers, cities, wars, archaeology and material culture surrounding it.

Suggest up to %d English Wikipedia article titles that a historian would cite for background on this passage.

Rules:
- Suggest titles for EXTERNAL history: empires, foreign rulers, cities, archaeological finds, primary inscriptions, customs, institutions, technology, geography.
- Do NOT suggest articles about the biblical book itself, its authorship or its theology.
- Do NOT suggest articles about biblical figures — the reader already has a dictionary article and a family tree for everyone the passage names. A foreign king known from outside sources (Sennacherib, Nebuchadnezzar II, Necho II) is an exception and is welcome; a figure known only from scripture (Esau, Ruth, Barnabas) is not.
- Prefer specific articles over general ones: "Siege of Lachish" over "Ancient warfare".
- Only suggest titles you believe actually exist on English Wikipedia.
- If the passage has no meaningful external historical context (most poetry, proverbs, and personal correspondence do not), return an empty list. An empty list is a good answer.

For each, give the exact article title and one short clause saying why it bears on this passage.`

const judgeInstruction = `A Bible reader is looking at this passage. Below are the openings of encyclopedia articles proposed as historical background for it.

For each one, decide whether it genuinely helps the reader understand the world behind this passage.

Keep an article only if it is about the historical, geographical, archaeological or cultural world surrounding the passage.

Reject it if:
- the article is about a different subject that happens to share a name (a modern place, a person from another period, a band, a ship)
- the article is about the Bible, biblical scholarship, or the passage itself rather than the world around it
- the connection is so general it would apply to any passage in the same era. "Chronology of the ancient Near East" and "4th millennium BC" are background to hundreds of passages and specific to none; reject them.
- the article's period is clearly wrong for the passage

Be strict. Rejecting a weak match costs the reader nothing; keeping one costs their trust.

For each article you keep, also classify it:
- "history" — the historical world itself: rulers attested outside the Bible, empires, wars, cities, archaeology, inscriptions, customs, technology, geography.
- "parallel" — another ancient text that resembles or shares motifs with this passage (Enūma Eliš, the Epic of Gilgamesh, Atra-Hasis, the Code of Hammurabi, the Sumerian King List). Anything presented as a literary or mythological comparison belongs here, not in "history".

Return one entry per article, using the index shown. When keeping, add one short clause (under 15 words) saying what it adds.`

func proposePrompt(b brief, max int) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, proposeInstruction, max)
	sb.WriteString("\n\n---\n")

	if b.Scope == "book" {
		fmt.Fprintf(&sb, "Book: %s\n", b.Title)
	} else {
		fmt.Fprintf(&sb, "Passage: %s\n", b.Reference)
		fmt.Fprintf(&sb, "Episode: %s\n", b.Title)
	}

	if year := formatYear(b.Year); year != "" {
		fmt.Fprintf(&sb, "Dated by the corpus to: %s (traditional chronology, may be approximate)\n", year)
	}
	if len(b.People) > 0 {
		fmt.Fprintf(&sb, "People named: %s\n", strings.Join(limit(b.People, 12), ", "))
	}
	if len(b.Places) > 0 {
		fmt.Fprintf(&sb, "Places named: %s\n", strings.Join(limit(b.Places, 12), ", "))
	}
	if b.Notes != "" {
		fmt.Fprintf(&sb, "Notes: %s\n", b.Notes)
	}

	return sb.String()
}

func judgePrompt(b brief, articles []*article) string {
	var sb strings.Builder

	sb.WriteString(judgeInstruction)
	sb.WriteString("\n\n---\n")

	if b.Scope == "book" {
		fmt.Fprintf(&sb, "Book: %s\n", b.Title)
	} else {
		fmt.Fprintf(&sb, "Passage: %s\nEpisode: %s\n", b.Reference, b.Title)
	}

	if year := formatYear(b.Year); year != "" {
		fmt.Fprintf(&sb, "Dated to: %s\n", year)
	}
	if len(b.People) > 0 {
		fmt.Fprintf(&sb, "People named: %s\n", strings.Join(limit(b.People, 12), ", "))
	}
	if len(b.Places) > 0 {
		fmt.Fprintf(&sb, "Places named: %s\n", strings.Join(limit(b.Places, 12), ", "))
	}

	for i, a := range articles {
		fmt.Fprintf(&sb, "\n[%d] %s", i, a.Title)
		if a.Description != "" {
			fmt.Fprintf(&sb, " — %s", a.Description)
		}
		fmt.Fprintf(&sb, "\n%s\n", truncate(a.Extract, 900))
	}

	return sb.String()
}

var proposeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"terms": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"why":   map[string]any{"type": "string"},
				},
				"required": []string{"query", "why"},
			},
		},
	},
	"required": []string{"terms"},
}

var judgeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"verdicts": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index":     map[string]any{"type": "integer"},
					"keep":      map[string]any{"type": "boolean"},
					"kind":      map[string]any{"type": "string", "enum": []string{"history", "parallel"}},
					"relevance": map[string]any{"type": "string"},
				},
				"required": []string{"index", "keep", "kind", "relevance"},
			},
		},
	},
	"required": []string{"verdicts"},
}

func propose(client llm, b brief, max int) ([]proposal, error) {
	raw, err := client.complete(proposePrompt(b, max), proposeSchema)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Terms []proposal `json:"terms"`
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode proposals: %w (%s)", err, truncate(string(raw), 200))
	}

	return limit(payload.Terms, max), nil
}

// judge scores every candidate for one passage in a single call.
//
// Judging one article at a time would be the obvious shape, but it multiplies
// the model calls by the candidate count, and the free tier is the binding
// constraint on how long a full run takes. Scoring them together also lets the
// model reject near-duplicates it would otherwise keep in isolation.
//
// The returned map is keyed by candidate index. An article the model says
// nothing about is absent, and callers treat absence as a rejection.
func judge(client llm, b brief, articles []*article) (map[int]verdict, error) {
	if len(articles) == 0 {
		return map[int]verdict{}, nil
	}

	raw, err := client.complete(judgePrompt(b, articles), judgeSchema)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Verdicts []verdict `json:"verdicts"`
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode verdicts: %w (%s)", err, truncate(string(raw), 200))
	}

	out := make(map[int]verdict, len(payload.Verdicts))

	for _, v := range payload.Verdicts {
		if v.Index >= 0 && v.Index < len(articles) {
			out[v.Index] = v
		}
	}

	return out, nil
}

// --- rate limiting ----------------------------------------------------------

// limiter spaces requests across every worker.
//
// Per-worker backoff is not enough on a free tier: six workers each politely
// waiting after their own 429 still arrive together, and the quota is
// account-wide rather than per-connection. Serialising the *issue* of requests
// through one clock is what keeps a long run under the limit.
type limiter struct {
	mu      sync.Mutex
	spacing time.Duration
	next    time.Time
}

func newLimiter(perMinute int) *limiter {
	if perMinute <= 0 {
		return &limiter{}
	}

	return &limiter{spacing: time.Minute / time.Duration(perMinute)}
}

func (l *limiter) wait() {
	if l == nil || l.spacing == 0 {
		return
	}

	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	sleep := l.next.Sub(now)
	l.next = l.next.Add(l.spacing)
	l.mu.Unlock()

	if sleep > 0 {
		time.Sleep(sleep)
	}
}

// penalise pushes every worker's next slot back after a rate-limit rejection,
// so one 429 slows the whole run rather than just the worker that hit it.
func (l *limiter) penalise(d time.Duration) {
	if l == nil || d <= 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	until := time.Now().Add(d)
	if l.next.Before(until) {
		l.next = until
	}
}

// --- Gemini -----------------------------------------------------------------

type geminiClient struct {
	apiKey  string
	model   string
	http    *http.Client
	limiter *limiter
}

func newGeminiClient(model string, perMinute int) (*geminiClient, error) {
	key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}

	return &geminiClient{
		apiKey:  key,
		model:   model,
		http:    &http.Client{Timeout: 180 * time.Second},
		limiter: newLimiter(perMinute),
	}, nil
}

func (g *geminiClient) name() string { return "gemini/" + g.model }

func (g *geminiClient) complete(prompt string, schema map[string]any) ([]byte, error) {
	body := map[string]any{
		"contents": []any{
			map[string]any{"parts": []any{map[string]any{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"temperature":      0,
			"responseMimeType": "application/json",
			"responseSchema":   schema,
			// These are classification jobs, not reasoning ones, and the free
			// tier is slow enough without paying for deliberation: at the
			// default level a single proposal call took ~24s against ~16s here.
			"thinkingConfig": map[string]any{"thinkingLevel": "low"},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.model, g.apiKey)

	var lastErr error

	// The free tier answers 429 and 503 often enough that this loop is the
	// difference between a run finishing and a run dying two hundred passages
	// in. Eight attempts with the server's own retry hint honoured costs
	// nothing on a healthy call and rescues most unhealthy ones.
	for attempt := range 8 {
		if attempt > 0 {
			time.Sleep(min(time.Duration(1<<attempt)*time.Second, 60*time.Second))
		}

		g.limiter.wait()

		request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")

		response, err := g.http.Do(request)
		if err != nil {
			lastErr = err
			continue
		}

		raw, err := io.ReadAll(response.Body)
		response.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("gemini HTTP %d: %s", response.StatusCode, truncate(string(raw), 200))

			if response.StatusCode == http.StatusTooManyRequests {
				// Gemini reports how long to wait; slowing every worker by that
				// much beats each of them rediscovering the limit separately.
				delay := retryDelay(raw)
				if delay == 0 {
					delay = 30 * time.Second
				}
				g.limiter.penalise(delay)
			}

			continue
		}

		var parsed struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}

		if err := json.Unmarshal(raw, &parsed); err != nil {
			lastErr = err
			continue
		}

		if len(parsed.Candidates) == 0 {
			lastErr = fmt.Errorf("gemini returned no candidates")
			continue
		}

		// Gemini 3 interleaves an encrypted thought-signature part with the
		// answer, so the text is whichever part actually carries text rather
		// than parts[0].
		var text strings.Builder

		for _, part := range parsed.Candidates[0].Content.Parts {
			text.WriteString(part.Text)
		}

		if strings.TrimSpace(text.String()) == "" {
			lastErr = fmt.Errorf("gemini returned empty text")
			continue
		}

		return []byte(text.String()), nil
	}

	return nil, lastErr
}

// retryDelay reads the "retryDelay" that Gemini attaches to a 429 as a
// RetryInfo detail, e.g. {"@type": ".../RetryInfo", "retryDelay": "27s"}.
func retryDelay(raw []byte) time.Duration {
	var payload struct {
		Error struct {
			Details []struct {
				Type       string `json:"@type"`
				RetryDelay string `json:"retryDelay"`
			} `json:"details"`
		} `json:"error"`
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}

	for _, detail := range payload.Error.Details {
		if !strings.Contains(detail.Type, "RetryInfo") {
			continue
		}

		if parsed, err := time.ParseDuration(strings.TrimSpace(detail.RetryDelay)); err == nil && parsed > 0 {
			return parsed
		}
	}

	return 0
}

// --- OpenAI -----------------------------------------------------------------

type openAIClient struct {
	apiKey  string
	model   string
	http    *http.Client
	limiter *limiter
}

func newOpenAIClient(model string, perMinute int) (*openAIClient, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	return &openAIClient{
		apiKey:  key,
		model:   model,
		http:    &http.Client{Timeout: 180 * time.Second},
		limiter: newLimiter(perMinute),
	}, nil
}

func (o *openAIClient) name() string { return "openai/" + o.model }

func (o *openAIClient) complete(prompt string, schema map[string]any) ([]byte, error) {
	// OpenAI's strict structured outputs require every property to be listed as
	// required and additional properties to be forbidden, at every level.
	strict := strictify(schema)

	body := map[string]any{
		"model": o.model,
		"input": prompt,
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "result",
				"strict": true,
				"schema": strict,
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	var lastErr error

	for attempt := range 8 {
		if attempt > 0 {
			time.Sleep(min(time.Duration(1<<attempt)*time.Second, 60*time.Second))
		}

		o.limiter.wait()

		request, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+o.apiKey)

		response, err := o.http.Do(request)
		if err != nil {
			lastErr = err
			continue
		}

		raw, err := io.ReadAll(response.Body)
		response.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("openai HTTP %d: %s", response.StatusCode, truncate(string(raw), 200))

			if response.StatusCode == http.StatusTooManyRequests {
				o.limiter.penalise(20 * time.Second)
			}

			continue
		}

		var parsed struct {
			Output []struct {
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}

		if err := json.Unmarshal(raw, &parsed); err != nil {
			lastErr = err
			continue
		}

		for _, item := range parsed.Output {
			if item.Type != "message" {
				continue
			}
			for _, part := range item.Content {
				if part.Type == "output_text" && strings.TrimSpace(part.Text) != "" {
					return []byte(part.Text), nil
				}
			}
		}

		lastErr = fmt.Errorf("openai returned no output text")
	}

	return nil, lastErr
}

// strictify walks a JSON schema adding the constraints OpenAI's strict mode
// demands. Gemini rejects additionalProperties, so this is applied only on the
// OpenAI path rather than baked into the shared schemas.
func strictify(schema map[string]any) map[string]any {
	out := map[string]any{}

	for key, value := range schema {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = strictify(typed)
		default:
			out[key] = value
		}
	}

	if out["type"] == "object" {
		out["additionalProperties"] = false

		if properties, ok := out["properties"].(map[string]any); ok {
			names := make([]string, 0, len(properties))
			for name := range properties {
				names = append(names, name)
			}
			out["required"] = names
		}
	}

	return out
}

func limit[T any](values []T, max int) []T {
	if len(values) <= max {
		return values
	}

	return values[:max]
}

func truncate(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}

	return string(runes[:max]) + "…"
}
