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

### Invite-code login

For testing and onboarding, the backend supports one-time invite codes.

#### Admin-only code creation

- `POST /api/v1/admin/invites`

This endpoint is protected and only works when the authenticated user matches `SCHOLIA_ADMIN_SUBJECT` or `SCHOLIA_ADMIN_USER_ID`.

Example admin env vars:

```bash
export SCHOLIA_ADMIN_SUBJECT="dev"
# or
export SCHOLIA_ADMIN_USER_ID="usr_xxx"
```

Security note: set one (or both) in deployment. If neither is set, admin-only routes return 503 (`Admin access not configured`).

#### Code exchange

- `POST /api/v1/auth/exchange-code`

Request body:

```json
{
  "code": "ABCD-EFGH-IJKL-MNOP"
}
```

On success, the server creates a private user and API key, then returns the API key once. The code cannot be reused.

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

Notes now require authentication for both read and write operations.

Read operations require `read` scope:

- `GET /api/v1/notes`
- `GET /api/v1/notes/{note_id}`

Write operations require `write` scope:

- `POST /api/v1/notes`
- `PUT /api/v1/notes/{note_id}`
- `DELETE /api/v1/notes/{note_id}`

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

## 11. Local Dev Auth Defaults

By default, bootstrap now requires explicit auth configuration.

Set one of these:

- `SCHOLIA_AUTH_KEYS`
- `SCHOLIA_AUTH_TOKEN`

For local-only development, you can opt in to a generated dev key:

```bash
export SCHOLIA_ALLOW_DEV_KEY=true
```

When enabled, bootstrap creates:

- token: `scholia-dev`

Recommended for real environments:

1. Set `SCHOLIA_AUTH_KEYS` with explicit keys and scopes.
2. Store keys in secrets manager, not in frontend code.
3. Set `SCHOLIA_ADMIN_USER_ID` (or `SCHOLIA_ADMIN_SUBJECT`) for admin-only invite minting.
4. Prefer a backend proxy for privileged operations.

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

# Admin mint invite (only if SCHOLIA_ADMIN_SUBJECT or SCHOLIA_ADMIN_USER_ID matches)
curl -s -X POST "http://localhost:8080/api/v1/admin/invites" \
  -H "X-API-Key: scholia-dev" \
  -H "Content-Type: application/json" \
  -d '{"label":"tester-one","scopes":["read","write"]}' | jq .

# Exchange invite code for a private API key
curl -s -X POST "http://localhost:8080/api/v1/auth/exchange-code" \
  -H "Content-Type: application/json" \
  -d '{"code":"ABCD-EFGH-IJKL-MNOP"}' | jq .
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

8. Invite-code onboarding was introduced

- New tester flow:
  - Admin mints one-time code: `POST /api/v1/admin/invites`
  - User exchanges code for API key: `POST /api/v1/auth/exchange-code`
- Codes are single-use and cannot be redeemed twice.

9. Admin gate for invite minting is env-driven

- Invite minting only works for authenticated principal matching:
  - `SCHOLIA_ADMIN_SUBJECT`, or
  - `SCHOLIA_ADMIN_USER_ID`
- Frontend should treat invite creation as a privileged admin action.

10. Lexicon endpoint now includes usage occurrences

- `GET /api/v1/lexicon/{strongs_id}` returns entry data plus `occurrences` from verse analysis.
- Frontend can render dictionary meaning and contextual usage from one request.

### Frontend migration checklist

1. Ensure all API calls parse the shared envelope and show `error.message` on failure.
2. Update note list/detail/create/update/delete calls to always send API key auth.
3. Add auth bootstrap on app load using `GET /api/v1/auth/me`.
4. Add invite-code login screen that posts to `POST /api/v1/auth/exchange-code`.
5. Store returned API key securely in client storage strategy used by your app.
6. Update verse, context, cross-reference, and analysis screens to handle range payloads.
7. Keep existing single-verse rendering path, but branch to range rendering when `verses` exists.
8. Update note editor to allow range references in `verse_ids`.
9. If admin UI exists, gate invite creation UI behind admin-authenticated state.

### Recommended frontend response handling pattern for verse endpoints

1. Detect range payload by checking `data.verses` and `data.start` plus `data.end`.
2. If absent, fall back to legacy single-verse fields.
3. For analysis/context range responses, prefer `analysis_by_verse` for grouped rendering.
4. Use legacy flattened `analysis` only for backward-compatible components.
