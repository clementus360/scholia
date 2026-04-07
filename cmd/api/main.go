package main

import (
	"log"
	"net/http"
	"os"

	"github.com/clementus360/scholia/internal/api"
	"github.com/clementus360/scholia/internal/auth"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/joho/godotenv"
)

func main() {
	envFiles := make([]string, 0, 2)
	for _, f := range []string{".env.local", ".env"} {
		if _, err := os.Stat(f); err == nil {
			envFiles = append(envFiles, f)
		}
	}
	if len(envFiles) > 0 {
		if err := godotenv.Load(envFiles...); err != nil {
			log.Printf("Failed to load env files: %v", err)
		}
	}

	// Initialize the DB
	db := storage.InitDB("./data/bible.db")
	defer db.Close()

	// Ensure tables exist
	storage.CreateTables(db)

	if err := auth.SeedBootstrapAuth(db); err != nil {
		log.Fatalf("failed to seed auth bootstrap data: %v", err)
	}

	authManager := auth.NewManager(db)

	// Create and configure router
	router := api.NewRouter(db, authManager)

	// Start server
	port := ":8080"
	log.Printf("Starting server on %s", port)
	log.Fatal(http.ListenAndServe(port, router))
}
