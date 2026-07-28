package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/clementus360/scholia/internal/api"
	"github.com/clementus360/scholia/internal/auth"
	"github.com/clementus360/scholia/internal/corpus"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/joho/godotenv"
)

func main() {
	loadEnvFiles()

	// Cancelled on SIGINT/SIGTERM. This also stops the background JWKS refresh
	// goroutine started by the auth verifier.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Bible corpus: read-only SQLite ---
	biblePath := storage.ResolveDBPath("./data/bible.db")
	if err := ensureCorpus(biblePath); err != nil {
		log.Fatalf("bible corpus: %v", err)
	}

	bible, err := storage.OpenBibleDB(biblePath)
	if err != nil {
		log.Fatalf("bible database: %v", err)
	}
	log.Printf("Bible corpus: %s (read-only)", biblePath)

	// --- User data: Supabase Postgres ---
	users, err := storage.InitUserDB(ctx)
	if err != nil {
		bible.Close()
		log.Fatalf("user database: %v", err)
	}
	log.Print("User data: Supabase Postgres (connected)")

	stores := &storage.Stores{Bible: bible, Users: users}
	defer stores.Close()

	// --- Supabase Auth ---
	cfg, err := auth.LoadConfig()
	if err != nil {
		log.Fatalf("supabase config: %v", err)
	}

	verifier, err := auth.NewVerifier(ctx, cfg)
	if err != nil {
		log.Fatalf("supabase jwt verifier: %v", err)
	}

	authManager := auth.NewManager(users, verifier)

	router := api.NewRouter(stores, authManager)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Starting server on :%s", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

// ensureCorpus builds the Bible corpus if it is not already there.
//
// The Docker build bakes the corpus into the image, so this normally does
// nothing. It matters on platforms that deploy from source without running the
// Dockerfile — a plain Go buildpack ships the repo's data/ directory but no
// database, and the server would otherwise refuse to start.
//
// The build takes roughly ten seconds and runs before the listener opens, so a
// cold start on such a platform is that much slower. Set
// SCHOLIA_AUTO_SEED=false to disable and require a prebuilt corpus.
func ensureCorpus(dbPath string) error {
	reason := ""

	switch _, err := os.Stat(dbPath); {
	case err == nil:
		// Present, but it may predate the current schema. An index added to
		// CreateBibleTables does not appear in a corpus built earlier, and the
		// only symptom is that the API is mysteriously slow — so treat a stale
		// corpus exactly like a missing one.
		if err := storage.CheckCorpusVersion(dbPath); err != nil {
			if !errors.Is(err, storage.ErrCorpusOutdated) {
				return err
			}
			reason = err.Error()
		} else {
			return nil
		}
	case os.IsNotExist(err):
		reason = fmt.Sprintf("no corpus at %s", dbPath)
	default:
		return fmt.Errorf("stat corpus at %s: %w", dbPath, err)
	}

	if disabled := strings.TrimSpace(os.Getenv("SCHOLIA_AUTO_SEED")); disabled != "" {
		if enabled, err := strconv.ParseBool(disabled); err == nil && !enabled {
			return fmt.Errorf(
				"%s, and SCHOLIA_AUTO_SEED is disabled. In a container this means the image was built "+
					"without an up-to-date corpus — rebuild the image so the seed stage runs again", reason)
		}
	}

	log.Printf("Corpus needs rebuilding: %s", reason)

	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale corpus: %w", err)
	}

	dataDir, err := corpus.ResolveDataDir()
	if err != nil {
		return fmt.Errorf("no corpus at %s and cannot build one: %w", dbPath, err)
	}

	log.Printf("Building corpus at %s from %s (this takes ~10s)...", dbPath, dataDir)
	started := time.Now()
	if err := corpus.Build(dbPath, dataDir); err != nil {
		return fmt.Errorf("build corpus: %w", err)
	}
	log.Printf("Corpus built in %s", time.Since(started).Round(time.Millisecond))

	return nil
}

// loadEnvFiles loads local .env files when present. In a container these will
// not exist and configuration comes from real environment variables instead.
func loadEnvFiles() {
	envFiles := make([]string, 0, 2)
	for _, f := range []string{".env.local", ".env"} {
		if _, err := os.Stat(f); err == nil {
			envFiles = append(envFiles, f)
		}
	}
	if len(envFiles) == 0 {
		return
	}
	if err := godotenv.Load(envFiles...); err != nil {
		log.Printf("Failed to load env files: %v", err)
	}
}
