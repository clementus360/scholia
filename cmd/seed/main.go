// Command seed builds the read-only Bible corpus from the source files under
// data/.
//
// The API builds the corpus automatically when it is missing, so this command
// is for rebuilding it deliberately — after changing the source data or the
// schema — and for the Docker build, which bakes the result into the image.
package main

import (
	"log"

	"github.com/clementus360/scholia/internal/corpus"
	"github.com/clementus360/scholia/internal/storage"
)

func main() {
	dataDir, err := corpus.ResolveDataDir()
	if err != nil {
		log.Fatalf("locate source data: %v", err)
	}

	dbPath := storage.ResolveDBPath("./data/bible.db")
	log.Printf("Building corpus at %s from %s", dbPath, dataDir)

	if err := corpus.Build(dbPath, dataDir); err != nil {
		log.Fatalf("build corpus: %v", err)
	}

	log.Print("🚀 Full Scholarly Suite Seeded Successfully!")
}
