package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

// EntityArticle is one article reached through a corpus entity.
//
// Relation is the field that matters most to a reader. "primary" means this
// article is *about* the entity — the person, the place itself. "nearby" means
// only that it sits on the same ground: within a couple of kilometres of an
// ancient site there is usually a modern village and often a nature reserve,
// and calling those the place would be a false claim. The UI quotes a primary
// and reduces the rest to a chip, so the difference stays visible.
type EntityArticle struct {
	ID          string `json:"id"`
	WikidataID  string `json:"wikidata_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Extract     string `json:"extract"`
	URL         string `json:"url"`
	Revision    int64  `json:"revision"`
	Retrieved   string `json:"retrieved"`
	License     string `json:"license"`
	Relation    string `json:"relation"`
	Confidence  int    `json:"confidence"`
	Method      string `json:"method"`
	Note        string `json:"note"`
}

// EntityNeighbour is one edge out of an article, offered as somewhere to go
// next. It carries no extract: the browser fetches that from Wikipedia when a
// reader actually asks.
type EntityNeighbour struct {
	TargetID string `json:"target_id"`
	Label    string `json:"label"`
	Relation string `json:"relation"`
}

// EntitySuggestion is everything the outside world offers about one thing this
// verse names, which is also how the UI groups it.
//
// Grouping by entity rather than by article is what keeps the panel honest and
// legible at once. It states why each suggestion is present — because the verse
// names Melchizedek — and it disappears when there is nothing to group: half of
// covered verses name a single entity, and for those this renders as one card
// with no visible structure at all.
type EntitySuggestion struct {
	Kind       string            `json:"kind"` // person | place | event | group
	EntityID   string            `json:"entity_id"`
	Name       string            `json:"name"`
	Articles   []EntityArticle   `json:"articles"` // primary first
	Neighbours []EntityNeighbour `json:"neighbours"`
}

// entitySources maps each kind to the tables that link it to a verse and name
// it. Kept as data rather than four near-identical functions, because the only
// thing that varies is which columns to join on.
var entitySources = []struct {
	Kind      string
	NameTable string
	NameCol   string
	IDCol     string
	// Query returning the entity ids this verse points at, taking the verse
	// lookup keys as its only arguments.
	LinkQuery string
}{
	{
		Kind: "person", NameTable: "people", NameCol: "name", IDCol: "id",
		LinkQuery: `SELECT DISTINCT pv.person_id FROM person_verses pv WHERE pv.verse_id IN (%s)`,
	},
	{
		Kind: "place", NameTable: "locations", NameCol: "name", IDCol: "id",
		LinkQuery: `SELECT DISTINCT vl.location_id FROM verse_locations vl WHERE vl.verse_id IN (%s)`,
	},
	{
		Kind: "event", NameTable: "events", NameCol: "title", IDCol: "id",
		LinkQuery: `SELECT DISTINCT ev.event_id FROM event_verses ev WHERE ev.verse_id IN (%s)`,
	},
	{
		Kind: "group", NameTable: "groups", NameCol: "name", IDCol: "id",
		LinkQuery: `SELECT DISTINCT gm.group_id FROM group_memberships gm
		            JOIN person_verses pv ON pv.person_id = gm.person_id
		            WHERE pv.verse_id IN (%s)`,
	},
}

// GetEntitySuggestions returns outside reading for everything a verse names.
//
// The corpus already records who and what each verse mentions — tens of
// thousands of such links — so this asks nothing new of the text. It walks from
// the verse to its entities, and from each entity to the articles cmd/resolve
// matched to it.
func GetEntitySuggestions(db *sql.DB, verseID string) ([]EntitySuggestion, error) {
	keys, err := getVerseLookupKeys(db, verseID)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}

	var suggestions []EntitySuggestion

	for _, source := range entitySources {
		found, err := suggestionsForKind(db, source.Kind, source.NameTable, source.NameCol,
			source.IDCol, fmt.Sprintf(source.LinkQuery, placeholders), args)
		if err != nil {
			return nil, fmt.Errorf("%s suggestions: %w", source.Kind, err)
		}

		suggestions = append(suggestions, found...)
	}

	if len(suggestions) == 0 {
		return nil, nil
	}

	if err := attachNeighbours(db, suggestions); err != nil {
		return nil, err
	}

	return suggestions, nil
}

func suggestionsForKind(db *sql.DB, kind, nameTable, nameCol, idCol, linkQuery string, args []any) ([]EntitySuggestion, error) {
	// Ordering puts each entity's primary first so the caller can quote it
	// without re-sorting, and orders entities by how well they resolved, so the
	// surest match leads the panel.
	query := fmt.Sprintf(`
		SELECT l.entity_id, COALESCE(n.%[1]s, ''),
		       a.id, COALESCE(a.wikidata_id, ''), COALESCE(a.title, ''),
		       COALESCE(a.description, ''), COALESCE(a.extract, ''), COALESCE(a.url, ''),
		       COALESCE(a.revision, 0), COALESCE(a.retrieved, ''), COALESCE(a.license, ''),
		       COALESCE(l.relation, 'nearby'), COALESCE(l.confidence, 0),
		       COALESCE(l.method, ''), COALESCE(l.note, '')
		FROM entity_article_links l
		JOIN external_articles a ON a.id = l.article_id
		JOIN %[2]s n ON n.%[3]s = l.entity_id
		WHERE l.entity_kind = ? AND l.entity_id IN (%[4]s)
		ORDER BY CASE l.relation WHEN 'primary' THEN 0 ELSE 1 END,
		         l.confidence DESC, a.title ASC`,
		nameCol, nameTable, idCol, linkQuery)

	full := append([]any{kind}, args...)

	rows, err := db.Query(query, full...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		ordered []string
		byID    = map[string]*EntitySuggestion{}
	)

	for rows.Next() {
		var (
			entityID, name string
			article        EntityArticle
		)

		if err := rows.Scan(&entityID, &name,
			&article.ID, &article.WikidataID, &article.Title, &article.Description,
			&article.Extract, &article.URL, &article.Revision, &article.Retrieved,
			&article.License, &article.Relation, &article.Confidence,
			&article.Method, &article.Note); err != nil {
			return nil, err
		}

		suggestion, ok := byID[entityID]
		if !ok {
			ordered = append(ordered, entityID)
			byID[entityID] = &EntitySuggestion{Kind: kind, EntityID: entityID, Name: name}
			suggestion = byID[entityID]
		}

		suggestion.Articles = append(suggestion.Articles, article)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]EntitySuggestion, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, *byID[id])
	}

	return out, nil
}

// attachNeighbours hangs the one-hop walk off each entity's primary article.
//
// Only the primary's neighbours are offered. Following the graph out of a
// merely-nearby article leads away from the passage rather than deeper into it,
// which is the opposite of what the reader asked for.
func attachNeighbours(db *sql.DB, suggestions []EntitySuggestion) error {
	wanted := map[string]bool{}

	for _, suggestion := range suggestions {
		for _, article := range suggestion.Articles {
			if article.Relation == "primary" {
				wanted[article.ID] = true
				break
			}
		}
	}

	if len(wanted) == 0 {
		return nil
	}

	ids := make([]any, 0, len(wanted))
	for id := range wanted {
		ids = append(ids, id)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := db.Query(fmt.Sprintf(`
		SELECT article_id, target_id, COALESCE(label, ''), COALESCE(relation, '')
		FROM article_neighbours
		WHERE article_id IN (%s)
		ORDER BY article_id, rank ASC`, placeholders), ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	byArticle := map[string][]EntityNeighbour{}

	for rows.Next() {
		var articleID string
		var neighbour EntityNeighbour

		if err := rows.Scan(&articleID, &neighbour.TargetID, &neighbour.Label, &neighbour.Relation); err != nil {
			return err
		}

		byArticle[articleID] = append(byArticle[articleID], neighbour)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	for i := range suggestions {
		for _, article := range suggestions[i].Articles {
			if article.Relation == "primary" {
				suggestions[i].Neighbours = byArticle[article.ID]
				break
			}
		}
	}

	return nil
}
