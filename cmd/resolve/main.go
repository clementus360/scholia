// Command resolve links the corpus's own people, places, events and groups to
// encyclopedia articles about them.
//
// It exists because cmd/harvest solved the wrong half of the problem. That pass
// asked a model which articles a *passage* was about, which cost money, covered
// 516 passages, and ignored the fact that the corpus already knows exactly who
// and what each verse names — 61,858 verse-to-entity links sitting in the
// database. Resolving the 5,000-odd entities once gives every verse that names
// one its own suggestions, and does it without a model.
//
// Nothing here generates text. Each entity is matched to a Wikidata item by
// evidence the corpus already holds — a coordinate for a place, a recorded
// family for a person — and the article's own opening is quoted with its
// revision id. An entity that cannot be matched gets nothing, which is the
// correct outcome for the many genealogy-only names no encyclopedia covers.
//
// Usage:
//
//	go run ./cmd/resolve -kind all -out data/world/entity-links.json -progress /tmp/resolve.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/clementus360/scholia/internal/storage"
	"github.com/clementus360/scholia/internal/wiki"
)

const userAgent = "Scholia/1.0 (https://github.com/clementus360/scholia; corpus entity resolver)"

func main() {
	var (
		dbPath   = flag.String("db", "./data/bible.db", "seeded corpus to read entities from")
		outPath  = flag.String("out", "data/world/entity-links.json", "where to write the compiled file")
		progPath = flag.String("progress", "", "JSONL checkpoint file; re-running skips entities already in it")
		kind     = flag.String("kind", "all", "all | people | places | events | groups")
		workers  = flag.Int("workers", 4, "entities resolved concurrently")
		limit    = flag.Int("limit", 0, "stop after this many entities (0 = all)")
		offset   = flag.Int("offset", 0, "skip this many entities first")
	)
	flag.Parse()

	db, err := storage.OpenBibleDB(*dbPath)
	if err != nil {
		log.Fatalf("open corpus: %v", err)
	}
	defer db.Close()

	entities, err := loadEntities(db, *kind)
	if err != nil {
		log.Fatalf("load entities: %v", err)
	}

	if *offset > 0 {
		if *offset >= len(entities) {
			log.Fatalf("offset %d is past the %d entities available", *offset, len(entities))
		}
		entities = entities[*offset:]
	}
	if *limit > 0 && *limit < len(entities) {
		entities = entities[:*limit]
	}

	done, err := loadDone(*progPath)
	if err != nil {
		log.Fatalf("read progress: %v", err)
	}

	pending := entities[:0:0]
	for _, e := range entities {
		if !done[e.Kind+"/"+e.ID] {
			pending = append(pending, e)
		}
	}

	log.Printf("Resolving %d entities (%d already done)", len(pending), len(entities)-len(pending))

	results := run(pending, *workers, *progPath)

	// Everything the progress file holds, not just this run's share, so an
	// interrupted run and its continuation compile to the same output.
	if *progPath != "" {
		replayed, err := readProgress(*progPath)
		if err != nil {
			log.Printf("⚠️ could not replay progress: %v", err)
		} else {
			results = replayed
		}
	}

	if err := writeOutput(*outPath, results); err != nil {
		log.Fatalf("write output: %v", err)
	}
}

// run resolves entities concurrently.
//
// A failure is logged and skipped rather than fatal. Over thousands of
// entities a handful of upstream timeouts is certain, and losing the rest to
// one of them would be absurd; re-running against the same progress file picks
// up only what is missing.
func run(entities []entity, workers int, progPath string) []result {
	wc := wiki.New(userAgent, workers)

	progress, err := openProgress(progPath)
	if err != nil {
		log.Fatalf("open progress: %v", err)
	}
	if progress != nil {
		defer progress.Close()
	}

	var (
		mu        sync.Mutex
		collected []result
		completed atomic.Int64
		wg        sync.WaitGroup
	)

	queue := make(chan entity)

	for range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for e := range queue {
				res, err := resolveOne(wc, e)
				if err != nil {
					log.Printf("  %s %q: %v", e.Kind, e.Name, err)
					continue
				}

				mu.Lock()
				collected = append(collected, res)
				if progress != nil {
					if encoded, err := json.Marshal(res); err == nil {
						fmt.Fprintln(progress, string(encoded))
					}
				}
				mu.Unlock()

				if n := completed.Add(1); n%100 == 0 {
					log.Printf("  %d/%d resolved", n, len(entities))
				}
			}
		}()
	}

	for _, e := range entities {
		queue <- e
	}
	close(queue)
	wg.Wait()

	return collected
}

func resolveOne(wc *wiki.Client, e entity) (result, error) {
	switch e.Kind {
	case "place":
		return resolvePlace(wc, e)
	case "person":
		return resolvePerson(wc, e)
	default:
		return resolveByName(wc, e, nil)
	}
}

// writeOutput folds every entity's work into one file, deduplicating articles.
//
// One article is reached from many entities — Jerusalem is named by hundreds of
// verses through dozens of them — so articles are keyed by id and stored once,
// with the links carrying the many-to-many part.
func writeOutput(path string, results []result) error {
	articles := map[string]*wiki.Article{}
	links := []entityLink{}
	seenLinks := map[string]bool{}
	neighbours := map[string][]neighbour{}

	for _, res := range results {
		for _, article := range res.Articles {
			if article != nil && article.ID != "" {
				articles[article.ID] = article
			}
		}
		for _, link := range res.Links {
			// A progress file can hold the same entity twice — an interrupted
			// run resumed, or two processes sharing one checkpoint — and the
			// results agree, so the second copy is simply dropped rather than
			// emitted as a duplicate row.
			key := link.Kind + "/" + link.EntityID + "/" + link.ArticleID
			if seenLinks[key] {
				continue
			}
			seenLinks[key] = true
			links = append(links, link)
		}
		for _, n := range res.Neighbours {
			neighbours[n.ArticleID] = append(neighbours[n.ArticleID], n)
		}
	}

	flatArticles := make([]*wiki.Article, 0, len(articles))
	for _, article := range articles {
		flatArticles = append(flatArticles, article)
	}
	sort.Slice(flatArticles, func(i, j int) bool { return flatArticles[i].ID < flatArticles[j].ID })

	sort.Slice(links, func(i, j int) bool {
		if links[i].Kind != links[j].Kind {
			return links[i].Kind < links[j].Kind
		}
		if links[i].EntityID != links[j].EntityID {
			return links[i].EntityID < links[j].EntityID
		}
		return links[i].Confidence > links[j].Confidence
	})

	flatNeighbours := []neighbour{}
	for _, id := range sortedKeys(neighbours) {
		seen := map[string]bool{}
		for _, n := range neighbours[id] {
			if seen[n.TargetID] {
				continue
			}
			seen[n.TargetID] = true
			flatNeighbours = append(flatNeighbours, n)
		}
	}

	payload := output{
		About: map[string]string{
			"method": "Each corpus entity was matched to a Wikidata item using evidence the corpus already holds: " +
				"a coordinate for a place, the recorded family for a person. No language model was involved at any " +
				"stage, and no text here was written by one — extracts are the source article's own opening.",
			"caution": "Matching is automatic. Every link records how it was made and how confident it is, and an " +
				"entity that could not be matched has no link rather than a guessed one.",
			"license": "Wikipedia text is CC BY-SA 4.0 and is attributed and linked per article. Wikidata identifiers are CC0.",
		},
		Articles:   flatArticles,
		Links:      links,
		Neighbours: flatNeighbours,
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return err
	}

	log.Printf("✅ %s: %d articles, %d links, %d neighbours", path, len(flatArticles), len(links), len(flatNeighbours))

	return nil
}

func sortedKeys(m map[string][]neighbour) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func openProgress(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}

	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

func loadDone(path string) (map[string]bool, error) {
	done := map[string]bool{}

	results, err := readProgress(path)
	if err != nil {
		return nil, err
	}

	for _, res := range results {
		done[res.Kind+"/"+res.EntityID] = true
	}

	return done, nil
}

func readProgress(path string) ([]result, error) {
	if path == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var results []result

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var res result
		if err := json.Unmarshal(line, &res); err != nil {
			continue
		}

		results = append(results, res)
	}

	return results, scanner.Err()
}
