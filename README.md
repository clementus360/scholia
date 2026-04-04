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
    "message": "Missing or invalid API key"
  }
}
```

Notes:

- `data` can be an object or array.
- `meta` appears on endpoints that support pagination.
- Standard HTTP status codes are used (200, 201, 400, 401, 403, 404, 500).

## 3. Authentication

Current auth is API-key based and intentionally simple so it can be upgraded to full user auth later.

### Send auth in one of these headers

- `X-API-Key: <token>`
- `Authorization: Bearer <token>`

### Auth check endpoint

- `GET /api/v1/auth/me`

Unauthenticated response:

```json
{
  "success": true,
  "data": {
    "authenticated": false
  }
}
```

Authenticated response example:

```json
{
  "success": true,
  "data": {
    "type": "api-key",
    "user_id": "usr_xxx",
    "key_id": "key_xxx",
    "subject": "dev",
    "display_name": "Dev",
    "scopes": ["read", "write"],
    "authenticated": true,
    "authentication": "api-key"
  }
}
```

### Protected routes

Currently, write operations for notes require `write` scope:

- `POST /api/v1/notes`
- `PUT /api/v1/notes/{note_id}`
- `DELETE /api/v1/notes/{note_id}`

Read routes remain public.

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

- `GET /notes?limit=...&offset=...`
- `GET /notes/{note_id}`
- `POST /notes` (auth required)
- `PUT /notes/{note_id}` (auth required)
- `DELETE /notes/{note_id}` (auth required)

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
};

type Group = { id: string; name: string };

type Event = {
  id: string;
  title: string;
  start_date: string;
  duration: string;
  sort_key: number;
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

// GET /analysis/{osis_id}
type VerseAnalysisData = {
  verse: Verse;
  analysis: VerseAnalysisToken[];
};
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
  cross_references: string[];
  notes: Note[];
};

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

## 11. Local Dev Auth Defaults

If no auth env vars are configured, bootstrap creates a dev API key:

- token: `scholia-dev`

Recommended for real environments:

1. Set `SCHOLIA_AUTH_KEYS` with explicit keys and scopes.
2. Store keys in secrets manager, not in frontend code.
3. Prefer a backend proxy for privileged operations.

## 12. Quick Test Commands

```bash
# Public read
curl -s "http://localhost:8080/api/v1/books?limit=2&offset=0" | jq .

# Auth session (anonymous)
curl -s "http://localhost:8080/api/v1/auth/me" | jq .

# Auth session (with key)
curl -s "http://localhost:8080/api/v1/auth/me" -H "X-API-Key: scholia-dev" | jq .

# Protected write
curl -s -X POST "http://localhost:8080/api/v1/notes" \
  -H "X-API-Key: scholia-dev" \
  -H "Content-Type: application/json" \
  -d '{"title":"Demo","main_reference":"GEN.1.1","content":"...","verse_ids":["GEN.1.1"]}' | jq .
```
