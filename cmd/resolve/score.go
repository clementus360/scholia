package main

import (
	"strings"

	"github.com/clementus360/scholia/internal/wiki"
)

// Wikidata classes that settle what a candidate is.
//
// Q20643955 is "human biblical figure", and it is the single most useful signal
// available: it appears on Melchizedek, King Saul, Solomon, Mary and Herod, and
// not on the footballer. Its absence proves nothing, though — Paul the Apostle
// carries only "human", because Wikidata models him as historically attested
// rather than as a figure known from scripture. So it scores, and never gates.
const classBiblicalFigure = "Q20643955"

// These, by contrast, are decisive in the negative. A name is not a person, and
// "Saul" the male given name outranks Saul the king in search.
var classNotAnEntity = map[string]bool{
	"Q12308941": true, // male given name
	"Q11879590": true, // female given name
	"Q202444":   true, // given name
	"Q3409032":  true, // unisex given name
	"Q101352":   true, // family name
	"Q4167410":  true, // disambiguation page
	"Q13406463": true, // list article
}

// Words that mark a description as belonging to the world of the corpus. Weak
// evidence on their own, which is why they are worth two points and genealogy
// is worth ten.
var biblicalWords = []string{
	"biblical", "bible", "hebrew", "israel", "judah", "judea", "testament",
	"apostle", "prophet", "patriarch", "priest", "evangelist", "disciple",
	"torah", "gospel", "jewish", "canaan", "samaria", "galilee",
}

// Scores at or above this are trusted enough to show a reader. One genealogy
// match, or a biblical class plus an exact name, clears it; a bare name match
// does not.
const acceptScore = 6

type scored struct {
	candidate  wiki.Candidate
	score      int
	method     string
	matchedVia string
}

// scorePerson ranks one search candidate against what the corpus knows.
//
// The ranking deliberately leans on facts the corpus already holds rather than
// on search position. Wikidata ranks "Saul" as a given name first, the apostle
// third and the king twelfth; nothing about that ordering knows which Saul a
// verse in 1 Samuel means. The corpus does: it knows his father was Kish.
func scorePerson(person entity, candidate wiki.Candidate, classes []string, related map[string]string) scored {
	out := scored{candidate: candidate, method: "name"}

	for _, class := range classes {
		if classNotAnEntity[class] {
			// A given name can never be the person, whatever else matches.
			return scored{candidate: candidate, score: -100, method: "rejected"}
		}
	}

	// Genealogy: the corpus's relations against Wikidata's, by label.
	overlap, sample := genealogyOverlap(person, related)
	if overlap > 0 {
		out.score += 10 * overlap
		out.method = "genealogy"
		out.matchedVia = sample
	}

	for _, class := range classes {
		if class == classBiblicalFigure {
			out.score += 6
			if out.method == "name" {
				out.method = "class"
			}
			break
		}
	}

	description := strings.ToLower(candidate.Description)
	for _, word := range biblicalWords {
		if strings.Contains(description, word) {
			out.score += 2
			break
		}
	}

	if strings.EqualFold(strings.TrimSpace(candidate.Label), strings.TrimSpace(person.Name)) {
		out.score++
	}

	return out
}

// genealogyOverlap counts relations the corpus and Wikidata agree on, and
// returns one of them to explain the match.
//
// Direction is ignored on purpose. The corpus stores "Saul child Jonathan" and
// Wikidata stores the same edge from either end depending on who was edited
// when; insisting the arrows point the same way would throw away good matches
// for a distinction neither source is careful about.
func genealogyOverlap(person entity, related map[string]string) (int, string) {
	if len(person.Relations) == 0 || len(related) == 0 {
		return 0, ""
	}

	corpusNames := map[string]string{}
	for relation, names := range person.Relations {
		for _, name := range names {
			corpusNames[normalizeName(name)] = relation
		}
	}

	count := 0
	sample := ""

	for _, label := range related {
		key := normalizeName(label)
		if relation, ok := corpusNames[key]; ok {
			count++
			if sample == "" {
				sample = relation + " " + label
			}
		}
	}

	return count, sample
}

// normalizeName strips the accents-and-hyphens noise that separates a corpus
// spelling from a Wikidata label — "Abel-beth-maacah" against "Abel-beth-maachah".
func normalizeName(value string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}

	return b.String()
}

// namesMatch is the looser test used for places, where the corpus name, the
// modern name and the Wikidata label are three spellings of one site.
func namesMatch(a, b string) bool {
	x, y := normalizeName(a), normalizeName(b)

	if x == "" || y == "" {
		return false
	}

	if x == y {
		return true
	}

	// One being a prefix of the other catches "Jerusalem" against
	// "Jerusalem, Israel" and "Tel Lachish" against "Lachish".
	return len(x) >= 5 && len(y) >= 5 && (strings.HasPrefix(x, y) || strings.HasPrefix(y, x))
}
