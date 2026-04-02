package main

import (
	"log"
	"net/http"

	"github.com/clementus360/scholia/internal/api"
	"github.com/clementus360/scholia/internal/auth"
	"github.com/clementus360/scholia/internal/storage"
)

func main() {
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
