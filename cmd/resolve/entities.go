package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// loadEntities reads the corpus rows worth trying to resolve.
//
// Only entities a verse actually points at are loaded. The corpus carries a
// long tail of genealogy-only names that appear in no verse's index, and
// resolving those would spend requests on suggestions no reader can ever reach.
func loadEntities(db *sql.DB, kind string) ([]entity, error) {
	var all []entity

	want := func(name string) bool { return kind == "all" || kind == name }

	if want("people") {
		people, err := loadPeople(db)
		if err != nil {
			return nil, fmt.Errorf("people: %w", err)
		}
		all = append(all, people...)
	}

	if want("places") {
		places, err := loadPlaces(db)
		if err != nil {
			return nil, fmt.Errorf("places: %w", err)
		}
		all = append(all, places...)
	}

	if want("events") {
		events, err := loadSimple(db, "event", `
			SELECT DISTINCT e.id, e.title
			FROM events e
			JOIN event_verses ev ON ev.event_id = e.id
			WHERE e.title IS NOT NULL AND e.title <> ''
			ORDER BY e.id`)
		if err != nil {
			return nil, fmt.Errorf("events: %w", err)
		}
		all = append(all, events...)
	}

	if want("groups") {
		groups, err := loadSimple(db, "group", `
			SELECT g.id, g.name
			FROM groups g
			WHERE g.name IS NOT NULL AND g.name <> ''
			ORDER BY g.id`)
		if err != nil {
			return nil, fmt.Errorf("groups: %w", err)
		}
		all = append(all, groups...)
	}

	return all, nil
}

// loadPeople brings each person's recorded family along with them, because that
// genealogy is the only thing that tells one Saul from another.
//
// People are ordered by how many verses name them, so a run cut short by -limit
// covers the ones readers meet most rather than an alphabetical slice.
func loadPeople(db *sql.DB) ([]entity, error) {
	rows, err := db.Query(`
		SELECT p.id, p.name, COALESCE(p.also_called, ''), COUNT(pv.verse_id) AS mentions
		FROM people p
		JOIN person_verses pv ON pv.person_id = p.id
		WHERE p.name IS NOT NULL AND p.name <> ''
		GROUP BY p.id
		ORDER BY mentions DESC, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var people []entity
	byID := map[string]int{}

	for rows.Next() {
		var (
			e          entity
			alsoCalled string
			mentions   int
		)

		if err := rows.Scan(&e.ID, &e.Name, &alsoCalled, &mentions); err != nil {
			return nil, err
		}

		e.Kind = "person"
		e.AlsoCalled = splitNames(alsoCalled)
		e.Relations = map[string][]string{}

		byID[e.ID] = len(people)
		people = append(people, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	relations, err := db.Query(`
		SELECT pr.person_id, pr.relation, q.name
		FROM person_relations pr
		JOIN people q ON q.id = pr.related_person_id
		WHERE q.name IS NOT NULL AND q.name <> ''`)
	if err != nil {
		return nil, err
	}
	defer relations.Close()

	for relations.Next() {
		var personID, relation, name string

		if err := relations.Scan(&personID, &relation, &name); err != nil {
			return nil, err
		}

		if index, ok := byID[personID]; ok {
			people[index].Relations[relation] = append(people[index].Relations[relation], name)
		}
	}

	return people, relations.Err()
}

// loadPlaces carries coordinates where the corpus has them, which is most of
// the time and is what makes places the most reliable thing here to resolve.
func loadPlaces(db *sql.DB) ([]entity, error) {
	rows, err := db.Query(`
		SELECT DISTINCT l.id, l.name, COALESCE(l.modern_name, ''), l.latitude, l.longitude
		FROM locations l
		JOIN verse_locations vl ON vl.location_id = l.id
		WHERE l.name IS NOT NULL AND l.name <> ''
		ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var places []entity

	for rows.Next() {
		var (
			e          entity
			modernName string
			lat, lon   sql.NullFloat64
		)

		if err := rows.Scan(&e.ID, &e.Name, &modernName, &lat, &lon); err != nil {
			return nil, err
		}

		e.Kind = "place"

		if modernName != "" {
			e.AlsoCalled = append(e.AlsoCalled, modernName)
		}

		// The corpus distinguishes the several places sharing a name by
		// numbering them — "Ramah 1", "Ramah 2", "Ramah 3". That suffix is the
		// corpus's own bookkeeping and appears in no encyclopedia, so the bare
		// name is carried alongside it for matching. Nearly a fifth of the
		// locations are numbered this way, and without this none of them could
		// ever match a Wikidata label.
		if base := strings.TrimRight(e.Name, "0123456789"); base != e.Name {
			if trimmed := strings.TrimSpace(base); trimmed != "" {
				e.AlsoCalled = append(e.AlsoCalled, trimmed)
			}
		}

		if lat.Valid && lon.Valid {
			e.Latitude, e.Longitude, e.HasCoords = lat.Float64, lon.Float64, true
		}

		places = append(places, e)
	}

	return places, rows.Err()
}

func loadSimple(db *sql.DB, kind, query string) ([]entity, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []entity

	for rows.Next() {
		e := entity{Kind: kind}

		if err := rows.Scan(&e.ID, &e.Name); err != nil {
			return nil, err
		}

		out = append(out, e)
	}

	return out, rows.Err()
}

// splitNames unpacks the comma-separated alternate names the source stores.
func splitNames(raw string) []string {
	var out []string

	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}
