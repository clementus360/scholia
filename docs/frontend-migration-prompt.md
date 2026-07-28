# Frontend migration prompt

Paste the block below into Claude Code from the **frontend** repo root.

---

The Scholia backend just replaced its authentication system. Update this
frontend to match. Inspect the existing code first and follow its conventions —
router, state management, styling, data-fetching layer — rather than
introducing new patterns.

## What changed on the backend

Auth used to be a single long-lived API key: the user pasted an invite code into
`POST /api/v1/auth/exchange-code`, got back a permanent `api_key`, and every
request sent it as `X-API-Key`.

That is gone. Identity is now **Supabase Auth**. Users sign in on the client with
Supabase directly; the backend only verifies the resulting JWT. There is no
sign-in, sign-up, or invite endpoint on the API any more — those routes return
404.

Access is **open**: anyone who signs up can use the app. Notes remain private to
their owner, enforced server-side.

## What to build

### 1. Supabase client

Install `@supabase/supabase-js` and create a single shared client. Read config
from environment variables — do not hardcode:

- `NEXT_PUBLIC_SUPABASE_URL` (or the equivalent public-prefix convention this
  project already uses)
- `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY`

The publishable key is `sb_publishable_…` and is safe to ship to the browser.
There is also a `sb_secret_…` key — **it must never appear in this repo**. If you
find one, stop and flag it.

Add the variables to `.env.example` (or equivalent) with empty values and a
comment saying where to get them. I will fill in the real values.

### 2. Auth pages and flows

- **Sign up** — email + password, plus "Continue with Google".
- **Sign in** — email + password, plus "Continue with Google".
- **OAuth callback route** — Google redirects back here; exchange the code for a
  session and send the user on to wherever they were heading.
- **Forgot password / reset password** — `resetPasswordForEmail` and the
  corresponding update-password screen.
- **Sign out**.
- **Email confirmation notice** — new email signups are unconfirmed until they
  click the link, and an unconfirmed user has no session. Say so explicitly
  instead of showing a generic failure.

Use `signInWithPassword`, `signUp`, `signInWithOAuth({ provider: 'google' })`,
and `signOut`.

Note: Google may not be enabled in the Supabase project yet. Build the button;
if the provider is disabled Supabase returns a clear error — surface it rather
than crashing.

### 3. Session handling

- Subscribe to `supabase.auth.onAuthStateChange` and keep session state in
  whatever store this app already uses. Do not cache the access token in
  component state or `localStorage` yourself — `supabase-js` owns refresh.
- Read the token via `await supabase.auth.getSession()` **at request time**, so
  you always send a fresh one. Tokens expire in an hour.
- Route guards: redirect unauthenticated users away from notes and settings, and
  redirect signed-in users away from sign-in/sign-up.
- On app load, call `GET /api/v1/auth/me` to bootstrap. It returns HTTP **200**
  with `{"authenticated": false}` when signed out — that is not an error, do not
  treat a 200 as proof of a session.

### 4. API client changes

Find the existing API wrapper and change how it authenticates:

- Remove all `X-API-Key` usage and any stored-API-key logic.
- Send `Authorization: Bearer ${session.access_token}` on authenticated
  requests.
- On a 401, refresh the session once and retry; if that fails, sign the user out
  and route to sign-in.

### 5. API keys settings page

Users can mint long-lived keys for scripts. Add a settings page that lists,
creates and revokes them.

Critical UX detail: the plaintext token is returned **exactly once**, at
creation, and cannot be retrieved later. Show it in a copyable panel with an
explicit "you will not see this again" warning. The list endpoint never returns
tokens.

These endpoints require a **session** — an API key cannot manage API keys, and
will get a 403 with that message.

## API contract

Base URL from an env var (`http://localhost:8080` in dev). All routes are under
`/api/v1`.

Every response uses the same envelope:

```ts
type ApiEnvelope<T> = {
  success: boolean
  data?: T
  error?: { message: string }
  meta?: { limit?: number; offset?: number; count?: number }
}
```

Always read `error.message` for failures — do not rely on status alone.

### Auth

```
GET /api/v1/auth/me
```
Signed out → 200 `{"success":true,"data":{"authenticated":false}}`
Signed in  → 200 with:
```ts
type Principal = {
  type: 'user' | 'api-key'
  user_id: string          // Supabase auth UUID
  subject: string
  display_name?: string
  email?: string
  scopes: string[]         // ['read','write']
  authenticated: boolean
  authentication: 'supabase-jwt' | 'api-key'
}
```

### API keys

```
GET    /api/v1/auth/api-keys
POST   /api/v1/auth/api-keys        { label: string, scopes?: ('read'|'write')[] }
DELETE /api/v1/auth/api-keys/{key_id}
```

```ts
type ApiKey = {
  id: string
  label: string
  scopes: string[]
  active: boolean
  last_used_at?: string   // RFC 3339
  created_at: string      // RFC 3339
  token?: string          // ONLY on create — never shown again
}
```
`DELETE` → `{"revoked": true}`. Revocation is immediate.

### Notes (auth required)

```
GET    /api/v1/notes?limit=&offset=
GET    /api/v1/notes/{note_id}
POST   /api/v1/notes
PUT    /api/v1/notes/{note_id}
DELETE /api/v1/notes/{note_id}
```

```ts
type Note = {
  id: number
  title: string
  main_reference: string
  content: string
  verse_ids?: string[]    // present on single-note reads, absent from list
  created_at: string      // RFC 3339
  updated_at: string      // RFC 3339
}
```

Create/update body: `{ title, main_reference, content, verse_ids }`.

`verse_ids` accepts human references **and ranges** — `"John 3:16"`,
`"John 3:16-18"`, `"GEN.1.1"` — and the server expands them to canonical OSIS
ids. An unresolvable reference returns 400 with
`"Unresolved verse reference(s): Hezekiah 9:9"`. Surface that message inline on
the field; it is the main way users learn a reference was mistyped.

### Public (no auth)

`/verse/{osis_id}`, `/verse/{osis_id}/context`,
`/verse/{osis_id}/cross-references`, `/analysis/{osis_id}`, `/search`,
`/suggest`, `/books`, `/books/{slug}/chapters`, `/timeline`,
`/lexicon/{strongs_id}`, `/location/*`, `/person/*`, `/group/*`, `/event/*`,
`/resolve/{rec_id}`.

Sending a valid token to these is still worthwhile — `/verse/{osis_id}/context`
includes the caller's own notes for that verse when authenticated.

### Error codes

| Code | Meaning |
| --- | --- |
| 401 | Missing or invalid credentials — refresh, then sign out |
| 403 | Insufficient permissions, or an API key attempted key management |
| 400 | Validation, including unresolvable verse references |
| 404 | Not found. Notes return 404 rather than 403 for someone else's note, deliberately |

## Breaking changes to hunt for

1. Anything reading, writing or sending a stored API key.
2. Any call to `POST /api/v1/auth/exchange-code` or `POST /api/v1/admin/invites`
   — both routes no longer exist.
3. Any invite-code entry UI.
4. Any admin-gated invite UI.
5. Timestamps: `created_at` / `updated_at` are now RFC 3339
   (`2026-07-28T06:11:36+02:00`), previously SQLite's `2026-07-28 06:11:36`.
   Fix any hand-rolled date parsing.
6. `user_id` is now a UUID, previously `usr_<hex>`.

## Constraints

- Do not build sign-in, sign-up, or password-reset endpoints against the Scholia
  API. All of that goes directly to Supabase from the client.
- Do not put a `sb_secret_…` key anywhere in this repo.
- Do not persist access tokens yourself.
- Match the existing styling and component conventions; do not add a UI library
  unless one is already in use.

## When done

List: files added or changed, env vars I need to fill in, anything you could not
verify without running against the live backend, and any pre-existing bug you
noticed but did not fix.
