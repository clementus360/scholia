package wiki

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Candidate is one possible match for a name, before anything has verified it.
//
// A name alone is rarely enough: "Saul" matches the king, the apostle, a given
// name and a footballer. Callers are expected to score candidates against
// something they already know — a genealogy, a coordinate, a date — rather than
// trusting search rank, which is why this carries the description and label
// instead of resolving straight to an article.
type Candidate struct {
	ID          string
	Label       string
	Description string
}

// Search returns the entities a term might name, best match first.
func (c *Client) Search(term string, limit int) ([]Candidate, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, nil
	}

	if limit <= 0 || limit > 50 {
		limit = 10
	}

	endpoint := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action":   {"wbsearchentities"},
		"search":   {term},
		"language": {"en"},
		"uselang":  {"en"},
		"format":   {"json"},
		"limit":    {strconv.Itoa(limit)},
	}.Encode()

	var payload struct {
		Search []struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"search"`
	}

	if err := c.get(endpoint, &payload); err != nil {
		return nil, err
	}

	found := make([]Candidate, 0, len(payload.Search))

	for _, item := range payload.Search {
		found = append(found, Candidate{
			ID:          item.ID,
			Label:       item.Label,
			Description: item.Description,
		})
	}

	return found, nil
}

// Claims returns, for each requested property, the entity ids it points at.
//
// Only entity-valued claims are returned. A property holding a date or a string
// is skipped rather than stringified, because every caller here is following
// edges in the graph rather than reading values off a node.
func (c *Client) Claims(entityID string, props ...string) (map[string][]string, error) {
	byEntity, err := c.ClaimsBatch([]string{entityID}, props...)
	if err != nil {
		return nil, err
	}

	return byEntity[entityID], nil
}

// ClaimsBatch is Claims for many entities at once.
//
// Scoring one ambiguous name means inspecting every candidate it returned.
// Asking about them one at a time turns a run over three thousand people into
// tens of thousands of round trips; wbgetentities takes fifty ids per request,
// so it becomes a few thousand instead.
func (c *Client) ClaimsBatch(entityIDs []string, props ...string) (map[string]map[string][]string, error) {
	if len(props) == 0 {
		return nil, nil
	}

	unique := make([]string, 0, len(entityIDs))
	seen := map[string]bool{}

	for _, id := range entityIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}

	out := map[string]map[string][]string{}

	for start := 0; start < len(unique); start += labelBatch {
		end := min(start+labelBatch, len(unique))

		if err := c.claimsPage(unique[start:end], props, out); err != nil {
			return out, err
		}
	}

	return out, nil
}

func (c *Client) claimsPage(ids []string, props []string, out map[string]map[string][]string) error {
	endpoint := "https://www.wikidata.org/w/api.php?" + url.Values{
		"action": {"wbgetentities"},
		"ids":    {strings.Join(ids, "|")},
		"props":  {"claims"},
		"format": {"json"},
	}.Encode()

	// datavalue.value is an object for entity references but a bare string for
	// external ids and the like, and a property can hold both. Decoding it
	// lazily keeps one string-valued claim from failing the whole entity.
	var payload struct {
		Entities map[string]struct {
			Claims map[string][]struct {
				Mainsnak struct {
					DataValue struct {
						Value json.RawMessage `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
			} `json:"claims"`
		} `json:"entities"`
	}

	if err := c.get(endpoint, &payload); err != nil {
		return err
	}

	for id, entity := range payload.Entities {
		found := map[string][]string{}

		for _, prop := range props {
			for _, claim := range entity.Claims[prop] {
				var value struct {
					ID string `json:"id"`
				}

				if err := json.Unmarshal(claim.Mainsnak.DataValue.Value, &value); err != nil {
					continue
				}

				if value.ID != "" {
					found[prop] = append(found[prop], value.ID)
				}
			}
		}

		if len(found) > 0 {
			out[id] = found
		}
	}

	return nil
}

// labelBatch is the most ids wbgetentities accepts in one request.
const labelBatch = 50

// Labels resolves entity ids to their English labels, batched.
//
// Ids without an English label are simply absent from the result; callers treat
// a missing label as an entity not worth showing, since an unlabelled node is
// not something a reader can be offered.
func (c *Client) Labels(ids []string) (map[string]string, error) {
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}

	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}

	out := map[string]string{}

	for start := 0; start < len(unique); start += labelBatch {
		end := min(start+labelBatch, len(unique))

		endpoint := "https://www.wikidata.org/w/api.php?" + url.Values{
			"action":    {"wbgetentities"},
			"ids":       {strings.Join(unique[start:end], "|")},
			"props":     {"labels"},
			"languages": {"en"},
			"format":    {"json"},
		}.Encode()

		var payload struct {
			Entities map[string]struct {
				Labels map[string]struct {
					Value string `json:"value"`
				} `json:"labels"`
			} `json:"entities"`
		}

		if err := c.get(endpoint, &payload); err != nil {
			return out, err
		}

		for id, entity := range payload.Entities {
			if label := entity.Labels["en"].Value; label != "" {
				out[id] = label
			}
		}
	}

	return out, nil
}

// GeoHit is an entity with coordinates, and how far it sits from a point.
type GeoHit struct {
	ID       string
	Label    string
	Distance float64 // kilometres
}

// Around returns entities within radiusKm of a point, nearest first.
//
// This is the strongest signal available for a place. The corpus carries
// coordinates for most of its locations, and a coordinate cannot be confused
// the way a name can: it identifies the site whatever the spelling, and the
// entities that come back alongside it — the tell, the modern town, the
// excavation — are genuine further reading rather than near-misses.
func (c *Client) Around(lat, lon, radiusKm float64, limit int) ([]GeoHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	// Point() takes longitude first, which is the opposite of how the corpus
	// stores it and an easy way to end up querying the wrong hemisphere.
	query := fmt.Sprintf(`SELECT ?item ?itemLabel ?dist WHERE {
  SERVICE wikibase:around {
    ?item wdt:P625 ?loc .
    bd:serviceParam wikibase:center "Point(%.6f %.6f)"^^geo:wktLiteral .
    bd:serviceParam wikibase:radius "%.3f" .
    bd:serviceParam wikibase:distance ?dist .
  }
  ?article schema:about ?item ; schema:isPartOf <https://en.wikipedia.org/> .
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
} ORDER BY ?dist LIMIT %d`, lon, lat, radiusKm, limit)

	endpoint := "https://query.wikidata.org/sparql?" + url.Values{
		"query":  {query},
		"format": {"json"},
	}.Encode()

	var payload struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}

	if err := c.get(endpoint, &payload); err != nil {
		return nil, err
	}

	hits := make([]GeoHit, 0, len(payload.Results.Bindings))

	for _, binding := range payload.Results.Bindings {
		id := binding["item"].Value
		if id == "" {
			continue
		}

		id = id[strings.LastIndex(id, "/")+1:]
		label := binding["itemLabel"].Value

		// An unlabelled hit comes back as its own id, which is no use to a
		// reader and means the item has no English label worth showing.
		if label == "" || label == id {
			continue
		}

		distance, _ := strconv.ParseFloat(binding["dist"].Value, 64)
		hits = append(hits, GeoHit{ID: id, Label: label, Distance: distance})
	}

	return hits, nil
}

// ArticleFor returns the article for an entity already identified by id.
//
// Resolve starts from a name and has to guess; this starts from an entity the
// caller has already verified, so it only has to find the English page and
// quote it. Results are cached by id, since one entity is reached from many
// verses.
func (c *Client) ArticleFor(entityID string) (*Article, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, nil
	}

	c.mu.Lock()
	if cached, ok := c.byEntity[entityID]; ok {
		c.mu.Unlock()
		return cached, nil
	}
	if c.missingEntity[entityID] {
		c.mu.Unlock()
		return nil, nil
	}
	c.mu.Unlock()

	page, err := c.englishPage(entityID)
	if err != nil || page == "" {
		if err == nil {
			c.mu.Lock()
			c.missingEntity[entityID] = true
			c.mu.Unlock()
		}
		return nil, err
	}

	found, err := c.summary(entityID, page)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if found == nil {
		c.missingEntity[entityID] = true
		return nil, nil
	}

	c.byEntity[entityID] = found

	return found, nil
}
