# Scholia Backend API Guide

This guide is for frontend developers integrating with the Scholia backend.

## 1. Base URL

- Local: `http://localhost:8080`
- API prefix: `/api/v1`

Example full URL:

```text
http://localhost:8080/api/v1/books
```

## 2. Response Contract

All endpoints use the same envelope.

### Success shape

```json
{
  "success": true,
  "data": {},
  "meta": {
    "limit": 20,
    "offset": 0,
    "count": 20
  }
}
```

### Error shape

```json
{
  "success": false,
  "error": {
    "message": "Missing or invalid credentials"
  }
}
```

Notes:

- `data` can be an object or array.
- `meta` appears on endpoints that support pagination.
- Standard HTTP status codes are used (200, 201, 400, 401, 403, 404, 500).

## 3. Authentication

Auth is Supabase-backed. Users sign in on the client with any enabled provider
and the API verifies the resulting access token locally against the project's
public signing keys — no per-request round trip, and no credential held by this
server.

### Send auth in one of these headers

- `Authorization: Bearer <token>` — a Supabase access token, or an API key
- `X-API-Key: <token>` — an API key

### Auth check endpoint

- `GET /api/v1/auth/me` — returns `authenticated:false` with HTTP 200 when
  signed out, so the frontend can call it unconditionally

### Sign in

Authentication is handled entirely by Supabase on the client. Sign in with
`supabase-js` using any enabled provider — email/password, Google, magic link,
passkeys — then send the resulting access token:

```
Authorization: Bearer <access_token>
```

`supabase-js` refreshes the token automatically. This API has no sign-up or
sign-in endpoint by design: routing credentials through it would mean managing
refresh tokens server-side for no benefit. It only verifies tokens, against
public keys, with no network round trip per request.

Enabling a new provider needs **no backend change**.

### API keys

For scripts and integrations that cannot run Supabase's refresh cycle.

- `GET /api/v1/auth/api-keys` — list your keys (tokens are never returned)
- `POST /api/v1/auth/api-keys` — mint one; the plaintext token is in the response **once**
- `DELETE /api/v1/auth/api-keys/{key_id}` — revoke, effective immediately

Send keys as `Authorization: Bearer …` or `X-API-Key: …`. Two deliberate
restrictions: a key cannot manage keys (those endpoints require a session), and
a key cannot hold scopes beyond `read` and `write`.

## 4. CORS for Local Testing

The backend includes a small CORS layer so browser-based frontend apps can call it from local dev servers.

### Default allowed origins

- `http://localhost:3000`
- `http://127.0.0.1:3000`
- `http://localhost:4173`
- `http://127.0.0.1:4173`
- `http://localhost:5173`
- `http://127.0.0.1:5173`
- `http://localhost:8080`
- `http://127.0.0.1:8080`

### Customize allowed origins

Set `SCHOLIA_CORS_ORIGINS` to a comma-separated list.

Example:

```bash
export SCHOLIA_CORS_ORIGINS="http://localhost:3000,http://localhost:5173"
```

### Notes

- `Authorization` and `X-API-Key` headers are allowed.
- Preflight `OPTIONS` requests are handled automatically.
- If you need to allow every origin temporarily, set `SCHOLIA_CORS_ORIGINS=*`.

## 5. Pagination

For paginated endpoints, use query params:

- `limit` (positive integer, capped per endpoint)
- `offset` (0 or more)

Example:

```text
GET /api/v1/books?limit=20&offset=40
```

Pagination metadata is returned in `meta`.

## 6. Canonical ID Behavior

- Verse IDs are normalized at the boundary (for example `gen.1.1` -> `GEN.1.1`).
- Book slugs are normalized to lowercase.
- Generic IDs are trimmed.

This helps clients send user-entered IDs without perfect casing.

## 7. Endpoint Map

### Verse and analysis

- `GET /verse/{osis_id}`
- `GET /verse/{osis_id}/context`
- `GET /verse/{osis_id}/cross-references`
- `GET /analysis/{osis_id}`

`GET /verse/{osis_id}`, `GET /verse/{osis_id}/context`, `GET /verse/{osis_id}/cross-references`, and `GET /analysis/{osis_id}` now accept either a single verse or a verse range.

Examples:

- Single verse: `GET /api/v1/verse/BSB.MAT.1.1`
- Human-readable single verse: `GET /api/v1/verse/John%201:1`
- Verse range: `GET /api/v1/verse/John%201:1-5`

Single-verse requests keep the existing response shape.

Range requests:

- `/verse/{osis_id}` returns `reference`, `start`, `end`, and `verses`.
- `/verse/{osis_id}/context` returns the same range fields plus aggregated entities (`people`, `groups`, `locations`, `events`, `lexicon`, `notes`, `cross_references`) and `analysis_by_verse`. It also includes legacy keys `verse` and flattened `analysis` for backward compatibility.
- `/verse/{osis_id}/cross-references` returns range fields plus `cross_references`, and includes `verse_id` for backward compatibility.
- `/analysis/{osis_id}` returns range fields plus `analysis_by_verse`, and also includes legacy keys `verse` and flattened `analysis`.

### Discovery

- `GET /search?q=...&type=all|verse|entity&limit=...&offset=...`
- `GET /suggest?q=...&limit=...&offset=...`

### Lexicon

- `GET /lexicon/{strongs_id}?limit=...&offset=...`

This route now returns the lexicon entry plus an `occurrences` array from verse analysis. That lets the frontend show both the meaning and the actual word usage.

### Geography

- `GET /location/{location_id}`
- `GET /location/{location_id}/verses?limit=...&offset=...`

### History

- `GET /person/{person_id}`
- `GET /person/{person_id}/verses?limit=...&offset=...`
- `GET /group/{group_id}`
- `GET /group/{group_id}/members?limit=...&offset=...`
- `GET /event/{event_id}`
- `GET /event/{event_id}/participants?limit=...&offset=...`

### Navigation

- `GET /books?limit=...&offset=...`
- `GET /books/{slug}/chapters`
- `GET /timeline?limit=...&offset=...`

### Resolve

- `GET /resolve/{rec_id}`

### Notes

- Notes are user-owned. Authenticated users only see their own notes.
- `GET /notes?limit=...&offset=...` (auth required)
- `GET /notes/{note_id}` (auth required)
- `POST /notes` (auth required)
- `PUT /notes/{note_id}` (auth required)
- `DELETE /notes/{note_id}` (auth required)
- Notes shown inside `/verse/{osis_id}/context` are also filtered to the authenticated user.

## 8. Response Structures

Below are practical TypeScript shapes for the most-used response payloads.

### Shared envelope

```ts
type ApiError = { message: string };

type ApiMeta = {
  limit?: number;
  offset?: number;
  count?: number;
  verses_count?: number;
  entities_count?: number;
  notes_count?: number;
  cross_references_count?: number;
  people_count?: number;
  groups_count?: number;
};

type ApiEnvelope<T> = {
  success: boolean;
  data?: T;
  error?: ApiError;
  meta?: ApiMeta;
};
```

### Core domain models

```ts
type Verse = {
  id: string;
  translation: string;
  book: string;
  chapter: number;
  verse: number;
  text: string;
};

type VerseRange = {
  reference: string;
  start: string;
  end: string;
  verses: Verse[];
};

type VerseRangeCrossRefs = {
  reference: string;
  start: string;
  end: string;
  verse_id?: string;
  cross_references: string[];
};

type VerseRangeAnalysis = {
  reference: string;
  start: string;
  end: string;
  verse?: Verse;
  verses: Verse[];
  analysis: VerseAnalysisToken[];
  analysis_by_verse: Record<string, VerseAnalysisToken[]>;
};

type VerseRangeContext = {
  reference: string;
  start: string;
  end: string;
  verse?: Verse;
  verses: Verse[];
  analysis: VerseAnalysisToken[];
  analysis_by_verse: Record<string, VerseAnalysisToken[]>;
  lexicon: LexiconEntry[];
  locations: Location[];
  people: Person[];
  groups: Group[];
  events: Event[];
  setting?: VerseSetting;
  world?: WorldContext;
  articles: DictionaryArticle[];
  cross_references: string[];
  notes: Note[];
};

type Note = {
  id: number;
  title: string;
  main_reference: string;
  content: string;
  verse_ids?: string[];
  created_at?: string;
  updated_at?: string;
};

type Person = {
  id: string;
  name: string;
  lookup_name: string;
  gender: string;
  birth_year: number;
  death_year: number;
  dictionary_text: string;
  slug: string;
  also_called?: string;   // comma separated alternate names
  birth_place?: string;
  death_place?: string;
  relations?: PersonRelation[];
};

type PersonRelation = {
  relation: "father" | "mother" | "child" | "sibling" | "partner";
  id: string;
  name: string;
};

type Group = { id: string; name: string };

type Event = {
  id: string;
  title: string;
  start_date: string;
  duration: string;
  sort_key: number;
  notes?: string;
  part_of?: { id: string; title: string };   // the larger episode
  follows?: { id: string; title: string };   // the event before it
  locations?: { id: string; name: string }[];
};

// When and where the passage sits. Every field is optional: the corpus dates
// about 90% of verses and names a writing place for only eleven books.
type VerseSetting = {
  year_num?: number;
  era?: {
    id: string;
    name: string;
    start_year: number;
    end_year: number;
    summary: string;
  };
  book?: {
    name: string;
    division?: string;
    testament?: string;
    year_written?: string;
    place_written?: string;
    writers?: string[];
  };
  // "verse" when the verse carries its own date, "book" when it does not and
  // the era was inferred from the rest of its book.
  era_source?: "verse" | "book";
};

// The world outside the passage: who ruled the surrounding powers during its
// era, what was happening elsewhere, and background pieces on the period.
//
// Joined by ERA, not by year. The verse years use a traditional chronology and
// these dates use the conventional one; they disagree by up to fifty years in
// the Old Testament. `year_aligned` is true only where the two can be compared
// directly (the New Testament), and only then are `current`/`nearby` set.
type WorldContext = {
  era_id: string;
  era_name: string;
  year_aligned: boolean;
  rulers: {
    id: string;
    name: string;
    title: string;
    region: string;
    start_year?: number;
    end_year?: number;
    note?: string;
    current: boolean;
  }[];
  events: {
    id: string;
    title: string;
    region: string;
    year?: number;
    summary?: string;
    nearby: boolean;
  }[];
  backgrounds: { id: string; region: string; title: string; body: string }[];
};

// Public-domain reference articles for terms the passage uses (Easton's, 1897).
// Articles about the verse's own people and places are excluded: those already
// travel on the person and location records.
type DictionaryArticle = {
  id: string;
  term: string;
  body: string;
  source: string;
  kind: string;
};

type Location = {
  id: string;
  name: string;
  modern_name: string;
  latitude?: number;
  longitude?: number;
  feature_type: string;
  geometry_type: string;
  image_file: string;
  image_url: string;
  credit_url: string;
  image_author: string;
  source_info: string;
};

type LexiconData = LexiconEntry & {
  occurrences: LexiconOccurrence[];
};

type LexiconOccurrence = {
  verse_id: string;
  word_order: number;
  surface_word: string;
  english_gloss: string;
  morph_code: string;
  manuscript_type: string;
  morphology?: MorphologyEntry;
};
```

### Lexicon example

```ts
type LexiconResponse = ApiEnvelope<LexiconData>;

const example: LexiconData = {
  strongs_id: "G3056",
  word: "λόγος",
  transliteration: "logos",
  definition: "word, saying, message, discourse",
  occurrences: [
    {
      verse_id: "1CO.1.18",
      word_order: 2,
      surface_word: "λόγος",
      english_gloss: "message",
      morph_code: "N-NSM",
      manuscript_type: "NKO",
      morphology: {
        code: "N-NSM",
        short_def: "Noun Nominative Singular Masculine",
        long_exp: "a male PERSON OR THING that is doing something",
      },
    },
  ],
};
```

Frontend usage pattern:

- Render `word` and `transliteration` in the header.
- Render `definition` as the main gloss.
- Render `occurrences` as a list or table of actual verse hits.
- Use `surface_word` + `english_gloss` to show word-by-word meaning, not just dictionary meaning.

### Endpoint-specific `data` shapes

```ts
// GET /books
type BooksData = Book[];
type Book = {
  id: string;
  osis_name: string;
  book_name: string;
  testament: string;
  book_order: number;
  slug: string;
};

// GET /books/{slug}/chapters
type BookChaptersData = {
  book: Book;
  chapter_count: number;
  chapters: Chapter[];
};
type Chapter = {
  id: string;
  book_id: string;
  osis_ref: string;
  chapter_num: number;
};

// GET /verse/{osis_id}
type VerseData = Verse;

// GET /verse/{osis_id}/cross-references
type VerseCrossRefsData = {
  verse_id: string;
  cross_references: string[];
};
type VerseCrossRefsRangeData = VerseRangeCrossRefs;

// GET /analysis/{osis_id}
type VerseAnalysisData = {
  verse: Verse;
  analysis: VerseAnalysisToken[];
};
type VerseAnalysisRangeData = VerseRangeAnalysis;
type VerseAnalysisToken = {
  word_order: number;
  surface_word: string;
  english_gloss: string;
  strongs_id: string;
  morph_code: string;
  manuscript_type: string;
  lexicon?: LexiconEntry;
  morphology?: MorphologyEntry;
};
type LexiconEntry = {
  strongs_id: string;
  word: string;
  transliteration: string;
  definition: string;
};
type MorphologyEntry = {
  code: string;
  short_def: string;
  long_exp: string;
};

// GET /verse/{osis_id}/context
type VerseContextData = {
  verse: Verse;
  analysis: VerseAnalysisToken[];
  lexicon: LexiconEntry[];
  locations: Location[];
  people: Person[];
  groups: Group[];
  events: Event[];
  setting?: VerseSetting;
  world?: WorldContext;
  articles: DictionaryArticle[];
  cross_references: string[];
  notes: Note[];
};
type VerseContextRangeData = VerseRangeContext;

// GET /search
type SearchData = {
  query: string;
  type: "all" | "verse" | "entity";
  verses?: SearchVerseResult[];
  entities?: SearchEntityResult[];
};
type SearchVerseResult = Verse;
type SearchEntityResult = {
  type: "person" | "location" | "event";
  id: string;
  name: string;
  extra?: string;
};

// GET /suggest
type SuggestData = {
  query: string;
  suggestions: Suggestion[];
};
type Suggestion = {
  type: "person" | "location" | "lexicon" | "event";
  id: string;
  value: string;
};

// GET /event/{event_id}/participants
type EventParticipantsData = {
  event_id: string;
  participants: {
    people: Person[];
    groups: Group[];
  };
};

// GET /auth/me
type AuthMeData =
  | { authenticated: false }
  | {
      type: "api-key";
      user_id: string;
      key_id: string;
      subject: string;
      display_name?: string;
      scopes: string[];
      authenticated: true;
      authentication: "api-key";
    };
```

## 9. Notes Payloads

### Create note request

```json
{
  "title": "Sermon notes",
  "main_reference": "GEN.1.1",
  "content": "In the beginning...",
  "verse_ids": ["GEN.1.1", "JHN.1.1"]
}
```

`verse_ids` supports both single references and ranges. Examples:

- `"JHN.1.1"`
- `"John 1:1"`
- `"John 1:1-5"`

Ranges are expanded server-side into individual verse IDs before persistence.

### Update note request

Same shape as create.

## 10. Frontend Integration Pattern

Use one HTTP helper to consistently handle auth and envelope parsing.

```ts
const API_BASE = "http://localhost:8080/api/v1";

type ApiEnvelope<T> = {
  success: boolean;
  data?: T;
  error?: { message: string };
  meta?: { limit?: number; offset?: number; count?: number };
};

async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
  apiKey?: string
): Promise<ApiEnvelope<T>> {
  const headers = new Headers(init.headers || {});
  headers.set("Content-Type", "application/json");
  if (apiKey) headers.set("X-API-Key", apiKey);

  const res = await fetch(`${API_BASE}${path}`, { ...init, headers });
  const json = (await res.json()) as ApiEnvelope<T>;

  if (!res.ok || !json.success) {
    const message = json.error?.message || `Request failed (${res.status})`;
    throw new Error(message);
  }

  return json;
}
```

## 11. Storage Architecture and Auth Setup

Scholia uses two databases with deliberately different lifecycles:

| Data | Store | Lifecycle |
| --- | --- | --- |
| Bible corpus, lexicon, geography, history | Read-only SQLite baked into the image | Disposable — rebuilt from `data/` by `cmd/seed` |
| Accounts, API keys, notes | Supabase Postgres | Durable — survives every deploy |

These previously shared one SQLite file, which was also committed to git.
Rebuilding the corpus — or just switching branches — destroyed user accounts and
notes. Keeping them apart is what fixes that. `data/bible.db` is now gitignored
and generated, never committed.

Required environment (see `.env.example` for the annotated version):

```bash
DATABASE_URL=…                    # Supabase Postgres, session pooler
SUPABASE_URL=…                    # https://<ref>.supabase.co
```

That is the whole server configuration. No Supabase API key is required: the API
only verifies tokens, using public keys. The publishable key belongs in your
frontend; the secret key is not used at all.

Full setup — project creation, applying `migrations/0001_init.sql`, enabling
asymmetric JWT signing keys, and creating the first admin — is in
[`docs/supabase-setup.md`](docs/supabase-setup.md).

The project **must** use asymmetric JWT signing keys. Access tokens are verified
locally against the project's public keys, which is what keeps authentication
free of a network round trip per request; a project still on the legacy shared
HS256 secret publishes no usable public key and every token will fail to verify.

## 12. Quick Test Commands

```bash
# Public read
curl -s "http://localhost:8080/api/v1/books?limit=2&offset=0" | jq .

# Auth session (anonymous)
curl -s "http://localhost:8080/api/v1/auth/me" | jq .

# Auth session (signed in). TOKEN is a Supabase access token —
# `supabase.auth.getSession()` in the browser, or the redemption response below.
curl -s "http://localhost:8080/api/v1/auth/me" -H "Authorization: Bearer $TOKEN" | jq .

# Protected write
curl -s -X POST "http://localhost:8080/api/v1/notes" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Demo","main_reference":"GEN.1.1","content":"...","verse_ids":["GEN.1.1"]}' | jq .

# Mint an API key for a script (session auth required)
curl -s -X POST "http://localhost:8080/api/v1/auth/api-keys" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"label":"my script","scopes":["read"]}' | jq .

# Use that key (the token is only shown once, at creation)
curl -s "http://localhost:8080/api/v1/notes" -H "X-API-Key: sk_scholia_..." | jq .

# Revoke it
curl -s -X DELETE "http://localhost:8080/api/v1/auth/api-keys/$KEY_ID" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

## 13. Frontend Change Log and Migration Notes

This section summarizes all frontend-relevant changes introduced in the recent backend updates.

### What changed

1. Unified API envelope is now standard everywhere

- Frontend should always parse responses as `{ success, data, error, meta }`.
- Error handling should read `error.message` instead of relying only on HTTP status.

2. Verse IDs and slugs are normalized at request boundaries

- Inputs like `gen.1.1` and `GEN.1.1` resolve consistently.
- Human-readable references like `John 1:1` are accepted by verse endpoints.

3. Verse range support was added across core verse surfaces

- The following now accept single verse or range:
  - `GET /api/v1/verse/{osis_id}`
  - `GET /api/v1/verse/{osis_id}/context`
  - `GET /api/v1/verse/{osis_id}/cross-references`
  - `GET /api/v1/analysis/{osis_id}`
- Range examples include `John 1:1-5`.

4. Backward compatibility fields were preserved for range responses

- Context and analysis range payloads still include legacy `verse` and flattened `analysis`.
- Cross-reference range payloads include legacy `verse_id`.
- Existing single-verse UI code should keep working while range-capable UI is added.

5. Notes became user-private and auth-scoped

- Notes are no longer globally shared.
- Authenticated users only see their own notes.
- Notes inside verse context are filtered by the current authenticated user.

6. Notes read routes are no longer public

- `GET /notes` and `GET /notes/{note_id}` now require auth with `read` scope.
- Existing frontend flows that loaded notes anonymously must now attach an API key.

7. Range references in note payloads are now supported

- `verse_ids` can include single references or ranges.
- Ranges are expanded server-side into individual verse IDs before save.

8. **Breaking:** authentication moved to Supabase, invites removed

- Sign-up and sign-in happen on the client via `supabase-js` with any enabled
  provider (email, Google, magic link, passkeys). This API has no sign-in route.
- Send `Authorization: Bearer <access_token>`; `supabase-js` handles refresh.
- The old permanent API key and the invite-code flow are gone, along with
  `POST /api/v1/auth/exchange-code` and `POST /api/v1/admin/invites`.
- Access is open: anyone signed in can read and write their own notes. Notes
  stay private to their owner.

9. API keys are now self-service

- `GET`/`POST /api/v1/auth/api-keys`, `DELETE /api/v1/auth/api-keys/{key_id}`.
- For scripts that cannot run a refresh cycle. Session-authenticated only; a key
  cannot mint or revoke keys.

10. Lexicon endpoint now includes usage occurrences

- `GET /api/v1/lexicon/{strongs_id}` returns entry data plus `occurrences` from verse analysis.
- Frontend can render dictionary meaning and contextual usage from one request.

### Frontend migration checklist

1. Ensure all API calls parse the shared envelope and show `error.message` on failure.
2. Install `supabase-js` and initialise it with your project URL and
   **publishable** key (`sb_publishable_…`). Never put the secret key in the
   frontend.
3. Replace the stored API key with a Supabase session. Build sign-in with
   whichever providers you enable — `signInWithPassword`, `signInWithOAuth({provider:'google'})`,
   `signInWithOtp` for magic links.
4. Attach `Authorization: Bearer ${session.access_token}` to every authenticated
   request, reading the session from `supabase.auth.getSession()` so you always
   get the refreshed token.
5. Subscribe to `supabase.auth.onAuthStateChange` to react to sign-in, sign-out
   and token refresh rather than caching the token yourself.
6. Add auth bootstrap on app load using `GET /api/v1/auth/me`. It returns
   `authenticated:false` with HTTP 200 when signed out, so it is safe to call
   unconditionally.
7. Update verse, context, cross-reference, and analysis screens to handle range payloads.
8. Keep existing single-verse rendering path, but branch to range rendering when `verses` exists.
9. Update note editor to allow range references in `verse_ids`.
10. Note `created_at` / `updated_at` are RFC 3339 (`2026-07-28T06:11:36+02:00`)
    rather than SQLite's `2026-07-28 06:11:36`.

### Recommended frontend response handling pattern for verse endpoints

1. Detect range payload by checking `data.verses` and `data.start` plus `data.end`.
2. If absent, fall back to legacy single-verse fields.
3. For analysis/context range responses, prefer `analysis_by_verse` for grouped rendering.
4. Use legacy flattened `analysis` only for backward-compatible components.
