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

## 4. Pagination

For paginated endpoints, use query params:

- `limit` (positive integer, capped per endpoint)
- `offset` (0 or more)

Example:

```text
GET /api/v1/books?limit=20&offset=40
```

Pagination metadata is returned in `meta`.

## 5. Canonical ID Behavior

- Verse IDs are normalized at the boundary (for example `gen.1.1` -> `GEN.1.1`).
- Book slugs are normalized to lowercase.
- Generic IDs are trimmed.

This helps clients send user-entered IDs without perfect casing.

## 6. Endpoint Map

### Verse and analysis

- `GET /verse/{osis_id}`
- `GET /verse/{osis_id}/context`
- `GET /verse/{osis_id}/cross-references`
- `GET /analysis/{osis_id}`

### Discovery

- `GET /search?q=...&type=all|verse|entity&limit=...&offset=...`
- `GET /suggest?q=...&limit=...&offset=...`

### Lexicon

- `GET /lexicon/{strongs_id}`

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

## 7. Notes Payloads

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

## 8. Frontend Integration Pattern

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

## 9. Local Dev Auth Defaults

If no auth env vars are configured, bootstrap creates a dev API key:

- token: `scholia-dev`

Recommended for real environments:

1. Set `SCHOLIA_AUTH_KEYS` with explicit keys and scopes.
2. Store keys in secrets manager, not in frontend code.
3. Prefer a backend proxy for privileged operations.

## 10. Quick Test Commands

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
