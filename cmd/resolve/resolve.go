package main

import (
	"fmt"
	"strings"

	"github.com/clementus360/scholia/internal/wiki"
)

// How far from a corpus coordinate a Wikidata site may sit and still be taken
// for the same place. Generous enough for a tell whose recorded position is the
// modern town beside it, tight enough that the next settlement over is a
// neighbour rather than a match.
const siteRadiusKm = 2.5

// Candidates pulled for an ambiguous name. King Saul ranks twelfth, so a narrow
// window is the difference between resolving him and losing him.
const nameCandidates = 20

// Neighbours kept per article, and the edges worth following.
const maxNeighbours = 8

var neighbourProps = []struct {
	Property string
	Label    string
}{
	{"P361", "part of"},
	{"P131", "in"},
	{"P17", "country"},
	{"P36", "capital"},
	{"P155", "follows"},
	{"P156", "followed by"},
	{"P39", "held position"},
	{"P22", "father"},
	{"P25", "mother"},
	{"P26", "spouse"},
	{"P40", "child"},
	{"P527", "includes"},
	{"P1441", "appears in"},
}

// resolvePlace matches a site by where it is rather than what it is called.
//
// This is the most reliable resolution in the pipeline and the least like the
// others. A coordinate survives every spelling the corpus and Wikidata disagree
// about, and what comes back alongside the match is not noise: querying Abel-
// beth-maacah returns the biblical city, the tell being excavated on top of it
// and the modern village beside it. Those are the deep dives a reader wants,
// and geometry found them without anything having to be asserted.
func resolvePlace(wc *wiki.Client, place entity) (result, error) {
	res := result{Kind: place.Kind, EntityID: place.ID, Name: place.Name}

	if !place.HasCoords {
		return resolveByName(wc, place, placeWords)
	}

	hits, err := wc.Around(place.Latitude, place.Longitude, siteRadiusKm, maxNeighbours)
	if err != nil {
		return res, fmt.Errorf("around: %w", err)
	}
	if len(hits) == 0 {
		return resolveByName(wc, place, placeWords)
	}

	// A coordinate says where to look, not what the place is called. Within a
	// couple of kilometres of an ancient site there is usually a modern village
	// and often a nature reserve, and the nearest labelled item is as likely to
	// be one of those as the site itself: querying around Ramah returns the town
	// of Moran, and around Allon a Mamluk caravanserai.
	//
	// So proximity alone never claims identity. Only a hit whose label matches
	// the name the corpus knows — or the modern name it records — becomes the
	// article *for* this place. Everything else is offered as what it actually
	// is: somewhere on the same ground.
	var primary *wiki.Article

	for _, hit := range hits {
		if !namesMatch(hit.Label, place.Name) && !anyNameMatches(hit.Label, place.AlsoCalled) {
			continue
		}

		article, err := wc.ArticleFor(hit.ID)
		if err != nil {
			return res, fmt.Errorf("article %s: %w", hit.ID, err)
		}
		if article == nil {
			continue
		}

		primary = article
		res.Articles = append(res.Articles, article)
		res.Links = append(res.Links, entityLink{
			Kind:       place.Kind,
			EntityID:   place.ID,
			ArticleID:  article.ID,
			Relation:   "primary",
			Confidence: 12,
			Method:     "coordinate+name",
			Note: fmt.Sprintf("The corpus places %s at %.4f, %.4f, where this site sits.",
				place.Name, place.Latitude, place.Longitude),
		})

		break
	}

	// One article can be returned twice by a radius query when an entity has
	// more than one coordinate statement, so links are deduplicated per entity.
	seen := map[string]bool{}
	if primary != nil {
		seen[primary.ID] = true
	}

	for _, hit := range hits {
		if len(res.Links) > maxNeighbours {
			break
		}

		nearby, err := wc.ArticleFor(hit.ID)
		if err != nil || nearby == nil || seen[nearby.ID] {
			continue
		}
		seen[nearby.ID] = true

		res.Articles = append(res.Articles, nearby)
		res.Links = append(res.Links, entityLink{
			Kind:       place.Kind,
			EntityID:   place.ID,
			ArticleID:  nearby.ID,
			Relation:   "nearby",
			Confidence: 4,
			Method:     "coordinate",
			Note: fmt.Sprintf("About %.1f km from where the corpus places %s. Nearby, not necessarily the same site.",
				hit.Distance, place.Name),
		})
	}

	if primary != nil {
		res.Neighbours = walkNeighbours(wc, primary)
	}

	return res, nil
}

// resolvePerson scores every candidate against the corpus's own genealogy.
func resolvePerson(wc *wiki.Client, person entity) (result, error) {
	res := result{Kind: person.Kind, EntityID: person.ID, Name: person.Name}

	candidates, err := wc.Search(person.Name, nameCandidates)
	if err != nil {
		return res, fmt.Errorf("search: %w", err)
	}
	if len(candidates) == 0 {
		return res, nil
	}

	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}

	// One batched call covers instance-of and the family edges for every
	// candidate, so scoring twenty of them costs a request rather than twenty.
	claims, err := wc.ClaimsBatch(ids, "P31", "P22", "P25", "P26", "P40", "P3373")
	if err != nil {
		return res, fmt.Errorf("claims: %w", err)
	}

	relativeIDs := []string{}
	for _, byProp := range claims {
		for prop, targets := range byProp {
			if prop != "P31" {
				relativeIDs = append(relativeIDs, targets...)
			}
		}
	}

	labels, err := wc.Labels(relativeIDs)
	if err != nil {
		return res, fmt.Errorf("labels: %w", err)
	}

	best := scored{score: -1000}

	for _, candidate := range candidates {
		byProp := claims[candidate.ID]

		related := map[string]string{}
		for prop, targets := range byProp {
			if prop == "P31" {
				continue
			}
			for _, target := range targets {
				if label, ok := labels[target]; ok {
					related[target] = label
				}
			}
		}

		got := scorePerson(person, candidate, byProp["P31"], related)
		if got.score > best.score {
			best = got
		}
	}

	if best.score < acceptScore {
		return res, nil
	}

	article, err := wc.ArticleFor(best.candidate.ID)
	if err != nil {
		return res, fmt.Errorf("article %s: %w", best.candidate.ID, err)
	}
	if article == nil {
		return res, nil
	}

	res.Articles = append(res.Articles, article)
	res.Links = append(res.Links, entityLink{
		Kind:       person.Kind,
		EntityID:   person.ID,
		ArticleID:  article.ID,
		Relation:   "primary",
		Confidence: best.score,
		Method:     best.method,
		Note:       personNote(best),
	})
	res.Neighbours = walkNeighbours(wc, article)

	return res, nil
}

func personNote(best scored) string {
	switch best.method {
	case "genealogy":
		if best.matchedVia != "" {
			return "Matched on the family the corpus records — " + best.matchedVia + "."
		}
		return "Matched on the family the corpus records."
	case "class":
		return "Matched by name; Wikidata records this entity as a biblical figure."
	default:
		return "Matched by name alone."
	}
}

// Words that mark a description as a place rather than a person or a song.
var placeWords = []string{
	"city", "town", "village", "region", "mountain", "mount", "river", "valley",
	"archaeological", "site", "ancient", "settlement", "kingdom", "province",
	"island", "desert", "lake", "sea", "spring", "well", "ruin", "tell",
}

// resolveByName is the fallback for entities with no coordinate and no
// genealogy — events, groups, and the few hundred places the corpus cannot fix
// on a map. It is the weakest path here, so it demands both a clean class and
// a description that reads like the right sort of thing.
func resolveByName(wc *wiki.Client, e entity, words []string) (result, error) {
	res := result{Kind: e.Kind, EntityID: e.ID, Name: e.Name}

	candidates, err := wc.Search(e.Name, 10)
	if err != nil {
		return res, fmt.Errorf("search: %w", err)
	}
	if len(candidates) == 0 {
		return res, nil
	}

	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}

	claims, err := wc.ClaimsBatch(ids, "P31")
	if err != nil {
		return res, fmt.Errorf("claims: %w", err)
	}

	for _, candidate := range candidates {
		if rejectedClass(claims[candidate.ID]["P31"]) {
			continue
		}

		description := strings.ToLower(candidate.Description)
		if !containsAny(description, words) && !containsAny(description, biblicalWords) {
			continue
		}
		if !namesMatch(candidate.Label, e.Name) && !anyNameMatches(candidate.Label, e.AlsoCalled) {
			continue
		}

		article, err := wc.ArticleFor(candidate.ID)
		if err != nil || article == nil {
			continue
		}

		res.Articles = append(res.Articles, article)
		res.Links = append(res.Links, entityLink{
			Kind:       e.Kind,
			EntityID:   e.ID,
			ArticleID:  article.ID,
			Relation:   "primary",
			Confidence: acceptScore,
			Method:     "name+description",
			Note:       "Matched by name; no coordinate or family in the corpus to check it against.",
		})
		res.Neighbours = walkNeighbours(wc, article)

		return res, nil
	}

	return res, nil
}

// walkNeighbours takes one hop out of an article, for readers who want to keep
// going. Labels only — the extract is a click away on Wikipedia's own CDN.
func walkNeighbours(wc *wiki.Client, article *wiki.Article) []neighbour {
	props := make([]string, 0, len(neighbourProps))
	for _, p := range neighbourProps {
		props = append(props, p.Property)
	}

	claims, err := wc.Claims(article.WikidataID, props...)
	if err != nil || len(claims) == 0 {
		return nil
	}

	targets := []string{}
	for _, p := range neighbourProps {
		targets = append(targets, claims[p.Property]...)
	}

	labels, err := wc.Labels(targets)
	if err != nil {
		return nil
	}

	out := []neighbour{}
	seen := map[string]bool{}

	for _, p := range neighbourProps {
		for _, target := range claims[p.Property] {
			label, ok := labels[target]
			if !ok || seen[target] || len(out) >= maxNeighbours {
				continue
			}

			seen[target] = true
			out = append(out, neighbour{
				ArticleID: article.ID,
				TargetID:  target,
				Label:     label,
				Relation:  p.Label,
				Rank:      len(out),
			})
		}
	}

	return out
}

func rejectedClass(classes []string) bool {
	for _, class := range classes {
		if classNotAnEntity[class] {
			return true
		}
	}

	return false
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}

	return false
}

func anyNameMatches(label string, names []string) bool {
	for _, name := range names {
		if namesMatch(label, name) {
			return true
		}
	}

	return false
}
