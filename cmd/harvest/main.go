// Command harvest compiles the external historical context shown in the
// History tab: encyclopedia articles about the world a passage sits in, matched
// to the passage and quoted with their source.
//
// It is deliberately an offline build step, not a runtime feature. The Bible
// does not change, so generating this per request would buy nothing and cost a
// model call, a Wikipedia round trip and a fresh chance to be wrong on every
// page view. Running it once produces a file that can be read end to end before
// it ships, and that every reader then sees identically.
//
// What the model does and does not do matters here. It proposes article titles
// and it filters the results; it never writes a sentence a reader sees. Every
// title has to resolve to a real Wikidata entity with a real English Wikipedia
// page or it is dropped, and the text stored is the article's own opening,
// quoted verbatim with its revision id. A hallucinated title therefore costs a
// wasted lookup rather than a false claim in the app.
//
// Usage:
//
//	go run ./cmd/harvest -env ~/path/to/.env -scope events -limit 20 -out /tmp/sample.json
//	go run ./cmd/harvest -env ~/path/to/.env -scope all -out data/world/passage-context.json
//
// The run is resumable: -progress writes one JSON line per finished passage, and
// re-running with the same progress file skips whatever is already in it.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clementus360/scholia/internal/storage"
	"github.com/clementus360/scholia/internal/wiki"
	"github.com/joho/godotenv"
)

// link joins one article to one passage, with the reason it was kept.
//
// Kind travels on the link rather than on the article because the same text can
// be either, depending on the passage it is set beside. The Code of Hammurabi
// is a parallel next to the covenant law codes and plain history next to a
// passage about Babylon.
type link struct {
	ArticleID string `json:"article_id"`
	Scope     string `json:"scope"`
	TargetID  string `json:"target_id"`
	Reference string `json:"reference"`
	Kind      string `json:"kind"`
	Relevance string `json:"relevance"`
	Rank      int    `json:"rank"`
}

// result is one passage's finished work, and the unit of resumability.
type result struct {
	Scope    string          `json:"scope"`
	TargetID string          `json:"target_id"`
	Articles []*wiki.Article `json:"articles"`
	Links    []link          `json:"links"`
}

type output struct {
	About    map[string]string `json:"_about"`
	Articles []*wiki.Article   `json:"articles"`
	Links    []link            `json:"links"`
}

func main() {
	var (
		envFile    = flag.String("env", "", "extra .env file to load (for GEMINI_API_KEY / OPENAI_API_KEY)")
		dbPath     = flag.String("db", "./data/bible.db", "seeded corpus to read passages from")
		outPath    = flag.String("out", "data/world/passage-context.json", "where to write the compiled file")
		progPath   = flag.String("progress", "", "JSONL checkpoint file; re-running skips passages already in it")
		scope      = flag.String("scope", "events", "events | books | all")
		provider   = flag.String("provider", "gemini", "gemini | openai")
		model      = flag.String("model", "", "model id (defaults per provider)")
		maxTerms   = flag.Int("max-terms", 4, "candidate articles to propose per passage")
		workers    = flag.Int("workers", 6, "passages processed concurrently")
		rpm        = flag.Int("rpm", 10, "model requests per minute across all workers (0 = unlimited)")
		limitCount = flag.Int("limit", 0, "stop after this many passages (0 = all)")
		offset     = flag.Int("offset", 0, "skip this many passages first")
	)

	flag.Parse()

	if *envFile != "" {
		if err := godotenv.Load(expandHome(*envFile)); err != nil {
			log.Fatalf("load %s: %v", *envFile, err)
		}
	}
	// A .env beside the repo still wins nothing over an already-set variable;
	// godotenv never overwrites.
	_ = godotenv.Load()

	client, err := newClient(*provider, *model, *rpm)
	if err != nil {
		log.Fatalf("model client: %v", err)
	}

	db, err := storage.OpenBibleDB(storage.ResolveDBPath(*dbPath))
	if err != nil {
		log.Fatalf("open corpus: %v", err)
	}
	defer db.Close()

	briefs, err := loadBriefs(db, *scope)
	if err != nil {
		log.Fatalf("load passages: %v", err)
	}

	if *offset > 0 && *offset < len(briefs) {
		briefs = briefs[*offset:]
	}
	if *limitCount > 0 && *limitCount < len(briefs) {
		briefs = briefs[:*limitCount]
	}

	done, err := loadProgress(*progPath)
	if err != nil {
		log.Fatalf("read progress: %v", err)
	}

	if len(done) > 0 {
		log.Printf("resuming: %d passages already done", len(done))
	}

	log.Printf("harvesting %d passages via %s (%d workers)", len(briefs), client.name(), *workers)

	results := run(client, db, briefs, done, *maxTerms, *workers, *progPath)

	if err := write(*outPath, results); err != nil {
		log.Fatalf("write %s: %v", *outPath, err)
	}
}

func newClient(provider, model string, rpm int) (llm, error) {
	switch strings.ToLower(provider) {
	case "gemini":
		if model == "" {
			model = "gemini-3.6-flash"
		}
		return newGeminiClient(model, rpm)
	case "openai":
		if model == "" {
			model = "gpt-5-mini"
		}
		return newOpenAIClient(model, rpm)
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
}

func loadBriefs(db *sql.DB, scope string) ([]brief, error) {
	switch strings.ToLower(scope) {
	case "events":
		return loadEventBriefs(db)
	case "books":
		return loadBookBriefs(db)
	case "all":
		events, err := loadEventBriefs(db)
		if err != nil {
			return nil, err
		}

		books, err := loadBookBriefs(db)
		if err != nil {
			return nil, err
		}

		return append(events, books...), nil
	default:
		return nil, fmt.Errorf("unknown scope %q", scope)
	}
}

// run walks the passages concurrently and returns whatever finished.
//
// A passage that fails is logged and skipped rather than fatal: over a run of
// hundreds, a handful of upstream timeouts is normal, and losing the other four
// hundred to one of them would be absurd. Re-running against the same progress
// file picks up only what is missing.
func run(client llm, db *sql.DB, briefs []brief, done map[string]bool, maxTerms, workers int, progPath string) []result {
	wc := wiki.New("Scholia/1.0 (https://github.com/clementus360/scholia; historical context harvester)", 4)

	var (
		mu        sync.Mutex
		collected []result
		completed int
		kept      int
	)

	progress, err := openProgress(progPath)
	if err != nil {
		log.Fatalf("open progress: %v", err)
	}
	if progress != nil {
		defer progress.Close()
	}

	queue := make(chan brief)
	var wg sync.WaitGroup
	started := time.Now()

	for range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for b := range queue {
				res, err := harvestOne(client, wc, b, maxTerms)

				mu.Lock()
				completed++
				position := completed

				if err != nil {
					log.Printf("[%d/%d] %s — failed: %v", position, len(briefs), b.label(), err)
					mu.Unlock()
					continue
				}

				kept += len(res.Links)
				collected = append(collected, res)

				if progress != nil {
					if encoded, err := json.Marshal(res); err == nil {
						fmt.Fprintln(progress, string(encoded))
					}
				}
				mu.Unlock()

				log.Printf("[%d/%d] %s — kept %d", position, len(briefs), b.label(), len(res.Links))
			}
		}()
	}

	for _, b := range briefs {
		if done[b.Scope+"/"+b.TargetID] {
			continue
		}

		queue <- b
	}

	close(queue)
	wg.Wait()

	log.Printf("done: %d passages, %d article links, %s elapsed", len(collected), kept, time.Since(started).Round(time.Second))

	// Earlier runs' work belongs in the output too, so a resumed run writes a
	// complete file rather than only its own share. Passages done in *this*
	// run are excluded: they were appended to the same progress file moments
	// ago, and replaying them would double every link.
	thisRun := make(map[string]bool, len(collected))
	for _, res := range collected {
		thisRun[res.Scope+"/"+res.TargetID] = true
	}

	replayed, err := replayProgress(progPath, briefs)
	if err != nil {
		log.Printf("⚠️ could not replay progress: %v", err)
		return collected
	}

	for _, res := range replayed {
		if !thisRun[res.Scope+"/"+res.TargetID] {
			collected = append(collected, res)
		}
	}

	return collected
}

// harvestOne runs the four steps for a single passage: propose, resolve, quote,
// judge.
func harvestOne(client llm, wc *wiki.Client, b brief, maxTerms int) (result, error) {
	res := result{Scope: b.Scope, TargetID: b.TargetID}

	proposals, err := propose(client, b, maxTerms)
	if err != nil {
		return res, fmt.Errorf("propose: %w", err)
	}

	var (
		candidates []*wiki.Article
		reasons    []string
	)

	for _, p := range proposals {
		found, err := wc.Resolve(p.Query)
		if err != nil {
			log.Printf("  resolve %q: %v", p.Query, err)
			continue
		}
		if found == nil {
			continue
		}

		// The same page can be reached by two different proposed terms.
		if slicesContains(candidates, found) {
			continue
		}

		candidates = append(candidates, found)
		reasons = append(reasons, p.Why)
	}

	if len(candidates) == 0 {
		return res, nil
	}

	verdicts, err := judge(client, b, candidates)
	if err != nil {
		return res, fmt.Errorf("judge: %w", err)
	}

	for i, candidate := range candidates {
		v, ok := verdicts[i]
		if !ok || !v.Keep {
			continue
		}

		relevance := strings.TrimSpace(v.Relevance)
		if relevance == "" {
			relevance = strings.TrimSpace(reasons[i])
		}

		// An unrecognised classification falls to "parallel", the more cautious
		// of the two: it is presented with a disclaimer, so a miscategorised
		// article understates its standing rather than overstating it.
		kind := strings.ToLower(strings.TrimSpace(v.Kind))
		if kind != "history" {
			kind = "parallel"
		}

		res.Articles = append(res.Articles, candidate)
		res.Links = append(res.Links, link{
			ArticleID: candidate.ID,
			Scope:     b.Scope,
			TargetID:  b.TargetID,
			Reference: b.Reference,
			Kind:      kind,
			Relevance: relevance,
			Rank:      len(res.Links),
		})
	}

	return res, nil
}

func slicesContains(articles []*wiki.Article, want *wiki.Article) bool {
	for _, a := range articles {
		if a.WikidataID == want.WikidataID {
			return true
		}
	}

	return false
}

// write flattens the per-passage results into one file: articles deduped by
// Wikidata id, links sorted so the output is stable across runs and a diff
// shows real changes rather than map ordering.
func write(path string, results []result) error {
	articles := map[string]*wiki.Article{}
	var links []link

	for _, res := range results {
		for _, a := range res.Articles {
			articles[a.ID] = a
		}
		links = append(links, res.Links...)
	}

	flat := make([]*wiki.Article, 0, len(articles))
	for _, a := range articles {
		flat = append(flat, a)
	}

	sort.Slice(flat, func(i, j int) bool { return flat[i].ID < flat[j].ID })
	sort.Slice(links, func(i, j int) bool {
		if links[i].Scope != links[j].Scope {
			return links[i].Scope < links[j].Scope
		}
		if links[i].TargetID != links[j].TargetID {
			return links[i].TargetID < links[j].TargetID
		}
		return links[i].Rank < links[j].Rank
	})

	payload := output{
		About: map[string]string{
			"purpose": "External historical context for a passage: encyclopedia articles about the empires, rulers, cities and material culture surrounding it, attached to the events and books they bear on.",
			"method":  "A language model proposed candidate article titles from what the corpus already knows about each passage, and filtered the results for relevance. Every title was then resolved against Wikidata and dropped unless it named a real entity with an English Wikipedia page. The model wrote none of the text below.",
			"text":    "Every extract is the opening of its Wikipedia article, quoted verbatim, with the revision id it was taken from. Nothing here is paraphrased or generated.",
			"license": "Wikipedia text is CC BY-SA 4.0 and is attributed and linked per article. Wikidata identifiers are CC0.",
			"caution": "Article selection is automatic and was reviewed rather than authored. The UI says so, and links out so a reader can check the source.",
			"scope":   "Attached to Theographic events (which carry their own verse links) and to books, never to a verse directly.",
		},
		Articles: flat,
		Links:    links,
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return err
	}

	log.Printf("wrote %s: %d articles, %d links", path, len(flat), len(links))

	return nil
}

// --- checkpointing ----------------------------------------------------------

func openProgress(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}

	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

func loadProgress(path string) (map[string]bool, error) {
	done := map[string]bool{}

	results, err := readProgress(path)
	if err != nil {
		return nil, err
	}

	for _, res := range results {
		done[res.Scope+"/"+res.TargetID] = true
	}

	return done, nil
}

// replayProgress returns earlier runs' results for the passages in this run, so
// a resumed run still writes a complete file rather than only the new part.
func replayProgress(path string, briefs []brief) ([]result, error) {
	if path == "" {
		return nil, nil
	}

	wanted := map[string]bool{}
	for _, b := range briefs {
		wanted[b.Scope+"/"+b.TargetID] = true
	}

	all, err := readProgress(path)
	if err != nil {
		return nil, err
	}

	var out []result
	seen := map[string]bool{}

	for _, res := range all {
		key := res.Scope + "/" + res.TargetID
		if !wanted[key] || seen[key] {
			continue
		}

		seen[key] = true
		out = append(out, res)
	}

	return out, nil
}

func readProgress(path string) ([]result, error) {
	if path == "" {
		return nil, nil
	}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []result

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var res result
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			continue
		}

		out = append(out, res)
	}

	return out, nil
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	return filepath.Join(home, path[2:])
}
