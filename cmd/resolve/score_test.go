package main

import "testing"

// The cases here are all real: every "should match" pair was produced by a run
// and checked by hand, and every "should not" was a false positive that shipped
// before the name gate existed.
func TestNameIsClose(t *testing.T) {
	cases := []struct {
		name       string
		alsoCalled []string
		label      string
		want       bool
	}{
		// Transliteration drift between the corpus and Wikidata.
		{name: "Tatnai", label: "Tattenai", want: true},
		{name: "Uzzia", label: "Uzziah", want: true},
		{name: "Urijah", label: "Uriah (prophet)", want: true},
		{name: "Elisabeth", label: "Elizabeth, mother of John the Baptist", want: true},
		// Arphaxad/Arpachshad is a correct pairing but four edits apart, which
		// no safe budget reaches. Genealogy is what rescues it, and this gate
		// does not gate genealogy.
		{name: "Arphaxad", label: "Arpachshad", want: false},

		// A qualifier in the label must not defeat the match.
		{name: "Joseph", label: "Joseph (Genesis)", want: true},
		{name: "Judah", label: "Judah (son of Jacob)", want: true},
		{name: "Peter", label: "Saint Peter", want: true},
		{name: "Paul", label: "Paul the Apostle", want: true},

		// The failure this gate exists for: a biblical figure, and the wrong one.
		{name: "God", label: "Mary, mother of Jesus", want: false},
		{name: "Nathan", label: "Bartholomew the Apostle", want: false},

		// Short names get a tighter budget, or every three-letter name in
		// scripture would match every other.
		{name: "Dan", label: "Ban", want: false},
		{name: "Dan", label: "Dan", want: true},

		// An alternate name the corpus records is as good as the name itself.
		{name: "Ramah 3", alsoCalled: []string{"Ramah"}, label: "Ramah", want: true},
	}

	for _, tc := range cases {
		got := nameIsClose(tc.label, entity{Name: tc.name, AlsoCalled: tc.alsoCalled})
		if got != tc.want {
			t.Errorf("nameIsClose(%q, %q) = %v, want %v", tc.label, tc.name, got, tc.want)
		}
	}
}

func TestWithinEdits(t *testing.T) {
	cases := []struct {
		a, b   string
		budget int
		want   bool
	}{
		{"uzzia", "uzziah", 2, true},
		{"tatnai", "tattenai", 2, true},
		{"god", "mary", 1, false},
		{"abc", "abc", 0, true},
		{"abc", "abd", 0, false},
		// Length alone settles it, without walking the matrix.
		{"a", "abcdefgh", 2, false},
	}

	for _, tc := range cases {
		if got := withinEdits(tc.a, tc.b, tc.budget); got != tc.want {
			t.Errorf("withinEdits(%q, %q, %d) = %v, want %v", tc.a, tc.b, tc.budget, got, tc.want)
		}
	}
}
