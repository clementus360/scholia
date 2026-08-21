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

// Reference works that only cover the world of the corpus. An entity one of
// these has an entry on is a biblical subject, whatever Wikidata's instance-of
// says — and this is what rescues the figures modelled as ordinary historical
// humans. Paul the Apostle is P31 "human" and nothing else, carries no family
// the corpus records, and so scored three out of the six he needed; his entry
// in the Bible Encyclopedia settles what he is.
//
// These ride the batch that already fetches instance-of, so the signal is free.
var sourceIsBiblical = map[string]bool{
	"Q889391":   true, // Easton's Bible Dictionary
	"Q48606171": true, // Easton's Bible Dictionary, 1897
	"Q653922":   true, // The Jewish Encyclopedia
	"Q4173137":  true, // Jewish Encyclopedia of Brockhaus and Efron
	"Q302556":   true, // The Catholic Encyclopedia
	"Q27062196": true, // Catholic Encyclopedia (New Advent)
	"Q4086271":  true, // Bible Encyclopedia of Archimandrite Nicephorus
	"Q21065550": true, // Dictionary of Biblical Criticism and Interpretation
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
func scorePerson(person entity, candidate wiki.Candidate, classes, sources []string, related map[string]string) scored {
	out := scored{candidate: candidate, method: "name"}

	for _, class := range classes {
		if classNotAnEntity[class] {
			// A given name can never be the person, whatever else matches.
			return scored{candidate: candidate, score: -100, method: "rejected"}
		}
	}

	// Genealogy: the corpus's relations against Wikidata's, by label.
	//
	// This is the one signal strong enough to stand without any agreement of
	// names, and it has to be, because the names it rescues are the ones a
	// reader could never match by hand: Israel is Jacob, Jehoiachin is
	// Jeconiah, Mattaniah is Zedekiah. Agreeing on a father and four children
	// identifies a person more surely than sharing a spelling does.
	overlap, sample := genealogyOverlap(person, related)
	if overlap > 0 {
		out.score += 10 * overlap
		out.method = "genealogy"
		out.matchedVia = sample
	}

	// Everything below is evidence about what the candidate *is*, not about
	// whether it is the entity in hand, so none of it counts unless the names
	// are at least close. Without this the class signal alone clears the bar,
	// and a search for "God" settles on Mary, mother of Jesus — a biblical
	// figure, correctly, and the wrong one entirely.
	if !nameIsClose(candidate.Label, person) {
		return out
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

	for _, source := range sources {
		if sourceIsBiblical[source] {
			out.score += 5
			if out.method == "name" {
				out.method = "reference"
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

// nameIsClose asks whether a candidate's label could be the corpus's name for
// the same person, allowing for the spelling drift between a nineteenth-century
// transliteration and a modern one.
//
// Exact agreement is far too strict: Tatnai is Tattenai, Uzzia is Uzziah,
// Urijah is Uriah and Elisabeth is Elizabeth, and all four are right. So the
// label is compared whole and token by token — "Uriah (prophet)" has to match
// on "Uriah" — with a small edit budget, scaled down for short names where two
// edits would reach halfway across the alphabet.
func nameIsClose(label string, person entity) bool {
	candidates := append([]string{person.Name}, person.AlsoCalled...)

	labelWhole := normalizeName(label)
	labelTokens := nameTokens(label)

	for _, name := range candidates {
		want := normalizeName(name)
		if want == "" {
			continue
		}

		budget := editBudget(want)

		if withinEdits(want, labelWhole, budget) {
			return true
		}

		for _, token := range labelTokens {
			if withinEdits(want, token, budget) {
				return true
			}
		}
	}

	return false
}

// editBudget scales the allowance to the length of the name.
//
// Short names get none. At three letters a single substitution reaches most of
// the rest of scripture — Dan, Ban, Nan — so anything but exact agreement there
// is a coincidence rather than a spelling. Longer names can afford more, since
// the chance of two unrelated names differing by two letters falls away quickly
// as they grow.
func editBudget(name string) int {
	switch {
	case len(name) <= 3:
		return 0
	case len(name) <= 5:
		return 1
	case len(name) <= 8:
		return 2
	default:
		return 3
	}
}

func nameTokens(label string) []string {
	var (
		out     []string
		current strings.Builder
	)

	flush := func() {
		if current.Len() > 0 {
			out = append(out, current.String())
			current.Reset()
		}
	}

	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			current.WriteRune(r)
		default:
			flush()
		}
	}

	flush()

	return out
}

// withinEdits reports whether a and b are within budget single-character edits.
//
// Levenshtein with an early width check: names differing by more than the
// budget in length cannot possibly be within it, and skipping those is most of
// the work when comparing one name against a long label.
func withinEdits(a, b string, budget int) bool {
	if a == b {
		return true
	}
	if len(a)-len(b) > budget || len(b)-len(a) > budget {
		return false
	}

	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)

	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(a); i++ {
		current[0] = i
		best := current[0]

		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
			best = min(best, current[j])
		}

		// Every remaining row can only add to the distance, so a row whose best
		// cell already exceeds the budget settles it.
		if best > budget {
			return false
		}

		previous, current = current, previous
	}

	return previous[len(b)] <= budget
}

// exactNameMatch is the strict test used to pick between sites at the same
// coordinate, where several may carry compatible names.
func exactNameMatch(label string, place entity) bool {
	want := normalizeName(label)

	if want == normalizeName(place.Name) {
		return true
	}

	for _, alias := range place.AlsoCalled {
		if want == normalizeName(alias) {
			return true
		}
	}

	return false
}
