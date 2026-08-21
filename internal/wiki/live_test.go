package wiki

import (
	"os"
	"testing"
)

// These tests talk to Wikidata and Wikipedia, so they are opt-in: a unit suite
// that fails when an upstream is slow is a suite people learn to ignore. Run
// them deliberately with SCHOLIA_LIVE_TEST=1 when changing the client.
func liveClient(t *testing.T) *Client {
	t.Helper()

	if os.Getenv("SCHOLIA_LIVE_TEST") == "" {
		t.Skip("set SCHOLIA_LIVE_TEST=1 to run tests that call Wikimedia")
	}

	return New("Scholia/1.0 (https://github.com/clementus360/scholia; test)", 2)
}

// A coordinate identifies a site whatever the corpus spells it, which is the
// whole reason places are resolved by position rather than by name.
func TestAroundFindsSiteByCoordinate(t *testing.T) {
	c := liveClient(t)

	hits, err := c.Around(33.258051, 35.581007, 3, 5)
	if err != nil {
		t.Fatalf("Around: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits near Abel-beth-maacah")
	}
	if hits[0].Distance > 1 {
		t.Errorf("nearest hit %q is %.2fkm away, expected under 1km", hits[0].Label, hits[0].Distance)
	}
	for _, hit := range hits {
		if hit.ID == "" || hit.Label == "" || hit.Label == hit.ID {
			t.Errorf("unlabelled hit survived filtering: %+v", hit)
		}
	}
}

// Search must return enough candidates for a caller to score, not just the top
// one: for ambiguous names the right entity is often well down the list.
func TestSearchReturnsRankedCandidates(t *testing.T) {
	c := liveClient(t)

	found, err := c.Search("Melchizedek", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("no candidates for Melchizedek")
	}
	if found[0].ID != "Q219395" {
		t.Errorf("top candidate = %q, want Q219395", found[0].ID)
	}
	if found[0].Description == "" {
		t.Error("candidate carries no description to score against")
	}
}

// The genealogy check is what keeps King Saul apart from Paul the Apostle.
//
// Search alone cannot do it: "Saul" ranks the apostle third and the king
// twelfth, behind two given names and a footballer. What separates them is that
// the corpus knows Saul's father was Kish and Wikidata knows the same.
func TestClaimsAndLabelsWalkGenealogy(t *testing.T) {
	c := liveClient(t)

	claims, err := c.Claims("Q28730", "P22", "P40") // King Saul: father, children
	if err != nil {
		t.Fatalf("Claims: %v", err)
	}
	if len(claims["P22"]) == 0 {
		t.Fatal("King Saul has no father claim")
	}

	labels, err := c.Labels(append(claims["P22"], claims["P40"]...))
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	if labels[claims["P22"][0]] != "Kish" {
		t.Errorf("father label = %q, want Kish", labels[claims["P22"][0]])
	}
}

// The king is far enough down the ranking that a narrow search window loses
// him, which is why the resolver casts wide and scores rather than trusting
// rank. This guards the assumption; if search ever surfaces him first, the
// resolver is still correct, but the wide net stops being load-bearing.
func TestAmbiguousNameNeedsAWideNet(t *testing.T) {
	c := liveClient(t)

	found, err := c.Search("Saul", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	position := -1
	for i, candidate := range found {
		if candidate.ID == "Q28730" {
			position = i
			break
		}
	}

	if position < 0 {
		t.Fatal("King Saul (Q28730) absent from 20 candidates")
	}
	if position < 3 {
		t.Logf("King Saul now ranks %d; the wide net is cheap insurance either way", position)
	}
}

func TestArticleForQuotesTheSource(t *testing.T) {
	c := liveClient(t)

	got, err := c.ArticleFor("Q219395")
	if err != nil {
		t.Fatalf("ArticleFor: %v", err)
	}
	if got == nil {
		t.Fatal("no article for Q219395")
	}
	if got.Extract == "" || got.Revision == 0 || got.URL == "" {
		t.Errorf("article is missing quotable provenance: %+v", got)
	}
	if got.WikidataID != "Q219395" {
		t.Errorf("WikidataID = %q, want Q219395", got.WikidataID)
	}
}
