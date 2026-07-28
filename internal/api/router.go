package api

import (
	"github.com/clementus360/scholia/internal/auth"
	"github.com/clementus360/scholia/internal/handlers"
	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter creates and configures the main router with all routes.
//
// Handlers are given only the databases they need: most read the Bible corpus
// alone, auth touches only Postgres, and notes/verse span both.
func NewRouter(stores *storage.Stores, authManager *auth.Manager) chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(httputil.CORS(nil))
	r.Use(authManager.Optional)

	// Initialize handlers
	verseHandler := handlers.NewVerseHandler(stores)
	lexiconHandler := handlers.NewLexiconHandler(stores.Bible)
	geographyHandler := handlers.NewGeographyHandler(stores.Bible)
	historyHandler := handlers.NewHistoryHandler(stores.Bible)
	notesHandler := handlers.NewNotesHandler(stores)
	discoveryHandler := handlers.NewDiscoveryHandler(stores.Bible)
	navigationHandler := handlers.NewNavigationHandler(stores.Bible)
	resolveHandler := handlers.NewResolveHandler(stores.Bible)
	authHandler := handlers.NewAuthHandler(stores.Users, authManager)

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

		// Auth endpoints.
		//
		// There is no sign-up or sign-in route here by design: the client does
		// those directly against Supabase (email, Google, magic link, ...) and
		// sends the resulting access token. This API only validates it.
		r.Get("/auth/me", authHandler.Me)

		// API keys, for clients that cannot refresh a session. Session-only —
		// see AuthHandler.sessionPrincipal.
		r.Group(func(r chi.Router) {
			r.Use(authManager.RequireScopes("read"))
			r.Get("/auth/api-keys", authHandler.ListAPIKeys)
			r.Post("/auth/api-keys", authHandler.CreateAPIKey)
			r.Delete("/auth/api-keys/{key_id}", authHandler.RevokeAPIKey)
		})

		// Notes endpoints
		r.Group(func(r chi.Router) {
			r.Use(authManager.RequireScopes("read"))
			r.Get("/notes", notesHandler.ListNotes)
			r.Get("/notes/{note_id}", notesHandler.GetNote)
		})
		r.Group(func(r chi.Router) {
			r.Use(authManager.RequireScopes("write"))
			r.Post("/notes", notesHandler.CreateNote)
			r.Put("/notes/{note_id}", notesHandler.UpdateNote)
			r.Delete("/notes/{note_id}", notesHandler.DeleteNote)
		})
	})

	return r
}
