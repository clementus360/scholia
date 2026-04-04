package api

import (
	"database/sql"

	"github.com/clementus360/scholia/internal/auth"
	"github.com/clementus360/scholia/internal/handlers"
	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter creates and configures the main router with all routes
func NewRouter(db *sql.DB, authManager *auth.Manager) chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(httputil.CORS(nil))
	if authManager != nil {
		r.Use(authManager.Optional)
	}

	// Initialize handlers
	verseHandler := handlers.NewVerseHandler(db)
	lexiconHandler := handlers.NewLexiconHandler(db)
	geographyHandler := handlers.NewGeographyHandler(db)
	historyHandler := handlers.NewHistoryHandler(db)
	notesHandler := handlers.NewNotesHandler(db)
	discoveryHandler := handlers.NewDiscoveryHandler(db)
	navigationHandler := handlers.NewNavigationHandler(db)
	resolveHandler := handlers.NewResolveHandler(db)
	authHandler := handlers.NewAuthHandler()

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Verse endpoints
		r.Get("/verse/{osis_id}", verseHandler.GetVerse)
		r.Get("/verse/{osis_id}/context", verseHandler.GetVerseContext)
		r.Get("/verse/{osis_id}/cross-references", verseHandler.GetVerseCrossReferences)
		r.Get("/analysis/{osis_id}", verseHandler.GetVerseAnalysis)

		// Lexicon endpoints
		r.Get("/lexicon/{strongs_id}", lexiconHandler.GetLexicon)

		// Discovery endpoints
		r.Get("/search", discoveryHandler.Search)
		r.Get("/suggest", discoveryHandler.Suggest)

		// Geography endpoints
		r.Get("/location/{location_id}", geographyHandler.GetLocation)
		r.Get("/location/{location_id}/verses", geographyHandler.GetLocationVerses)

		// Historical endpoints
		r.Get("/person/{person_id}", historyHandler.GetPerson)
		r.Get("/person/{person_id}/verses", historyHandler.GetPersonVerses)
		r.Get("/group/{group_id}", historyHandler.GetGroup)
		r.Get("/group/{group_id}/members", historyHandler.GetGroupMembers)
		r.Get("/event/{event_id}", historyHandler.GetEvent)
		r.Get("/event/{event_id}/participants", historyHandler.GetEventParticipants)

		// Navigation endpoints
		r.Get("/books", navigationHandler.GetBooks)
		r.Get("/books/{slug}/chapters", navigationHandler.GetBookChapters)
		r.Get("/timeline", navigationHandler.GetTimeline)

		// Resolver endpoint
		r.Get("/resolve/{rec_id}", resolveHandler.ResolveRecID)
		r.Get("/auth/me", authHandler.Me)

		// Notes endpoints
		r.Get("/notes", notesHandler.ListNotes)
		r.Get("/notes/{note_id}", notesHandler.GetNote)
		r.Group(func(r chi.Router) {
			r.Use(authManager.RequireScopes("write"))
			r.Post("/notes", notesHandler.CreateNote)
			r.Put("/notes/{note_id}", notesHandler.UpdateNote)
			r.Delete("/notes/{note_id}", notesHandler.DeleteNote)
		})
	})

	return r
}
