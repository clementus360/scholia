# Supabase setup

Scholia stores its two datasets separately, because they have opposite
lifecycles:

| Data | Store | Lifecycle |
| --- | --- | --- |
| Bible corpus, lexicon, geography, history | Read-only SQLite baked into the container image | Disposable. Rebuilt from `data/` whenever the corpus changes. |
| Accounts, notes, API keys | Supabase Postgres | Durable. Must survive every deploy. |

These used to share one SQLite file, which was also committed to git. Rebuilding
the corpus — or simply checking out another branch — destroyed user accounts and
notes. Keeping them apart is what fixes that.

---

## How authentication works

Identity is entirely Supabase's job. Sign-up, sign-in, OAuth, token refresh and
password reset all happen **directly between the client and Supabase**. The API
never sees a password and never acts on a user's behalf.

All it does is verify the access token on each request, against public keys
fetched from the project's JWKS endpoint. That verification is local — no
network round trip per request — which is what makes it cheap enough to sit in
middleware.

Two consequences worth knowing:

- **The server needs no Supabase API key.** Not the publishable key, not the
  secret key. The publishable key belongs in your frontend config. Its only
  credential is the Postgres connection string.
- **Any enabled provider works with no backend change.** Turn on Google, GitHub,
  magic links or passkeys in the dashboard and they work against this API
  immediately, because the API only cares that the token is validly signed.

Access is **open**: anyone who signs in with an enabled provider can read and
write their own notes. Notes are private to their owner, enforced in every
query by `owner_user_id`.

> If you later want to close access, do **not** reintroduce an invite table as
> an account-creation gate. Supabase will still let anyone create an account
> directly through the publishable key, which ships in your frontend. Gate
> *authorization* instead: add an approval column to `profiles` and check it
> when building the principal in `internal/auth/manager.go`.

---

## 1. Create the project

Create a Supabase project, then collect:

| Value | Location | Env var |
| --- | --- | --- |
| Project URL | Settings → API | `SUPABASE_URL` |
| Connection string | Settings → Database | `DATABASE_URL` |

That is the entire server configuration.

For `DATABASE_URL`, prefer the **session pooler**. The direct connection
(`db.<ref>.supabase.co`) is IPv6-only and unreachable from most container
platforms — it resolves on macOS, so it is easy to get working locally and then
fail on deploy. If you use the transaction pooler (`:6543`), append
`?default_query_exec_mode=simple_protocol`; server-side prepared statements do
not survive it. The API refuses to boot on a `:6543` DSN without that parameter
rather than failing unpredictably later.

URL-encode special characters in the password. An unencoded `@` terminates the
userinfo section and the DSN will not parse: `@` → `%40`, `!` → `%21`,
`#` → `%23`.

## 2. Enable asymmetric JWT signing keys

**This is required.** A project still using the legacy shared HS256 secret
publishes no usable public key, and every token will fail to verify.

Dashboard → **Authentication → JWT Keys → migrate to asymmetric signing keys**
(ES256 or RS256).

## 3. Apply the schema

```bash
psql "$DATABASE_URL" -f migrations/0001_init.sql
psql "$DATABASE_URL" -f migrations/0002_open_signup.sql
```

This creates `profiles`, `api_keys`, `notes` and `note_verses`, enables RLS, and
installs a trigger that creates a profile row whenever a Supabase Auth user is
created — including users created through OAuth or the dashboard.

## 4. Enable the providers you want

Dashboard → **Authentication → Providers**. Email is on by default. For Google
you will need an OAuth client ID and secret from the Google Cloud console, and
the Supabase callback URL it shows you.

No backend change is needed for any of them.

## 5. Run

```bash
go run ./cmd/seed   # builds data/bible.db, ~10s
go run ./cmd/api
```

In production the corpus is generated inside the Docker build and baked into the
image, so no volume is needed:

```bash
docker build -t scholia-api .
docker run --rm -p 8080:8080 --env-file .env scholia-api
```

---

## API surface for clients

**Sign in** with `supabase-js` (or any Supabase client), then send the access
token:

```
Authorization: Bearer <access_token>
```

`supabase-js` handles refresh automatically. There is no sign-in endpoint on
this API by design — routing credentials through it would mean managing refresh
tokens server-side for no benefit.

| Endpoint | Purpose |
| --- | --- |
| `GET /api/v1/auth/me` | Who am I? Returns `authenticated:false` (200, not 401) when signed out. |
| `GET /api/v1/auth/api-keys` | List your API keys. Tokens are never returned. |
| `POST /api/v1/auth/api-keys` | Mint a key. `{"label":"…","scopes":["read","write"]}`. The plaintext token is in the response **once**. |
| `DELETE /api/v1/auth/api-keys/{id}` | Revoke a key. Takes effect immediately. |

### API keys

For scripts and integrations that cannot run Supabase's refresh cycle. Send them
as `Authorization: Bearer …` or `X-API-Key: …` — the API distinguishes them from
JWTs by shape, so no separate header is needed.

Two deliberate restrictions:

- **A key cannot manage keys.** Key endpoints require a session. A leaked key
  that could mint further keys would outlive its own revocation.
- **A key cannot exceed the scopes of a session** (`read`, `write`).

Revocation evicts the key from the authentication cache immediately. Note this
is per-process: if you run more than one instance, the others honour the key
until their cache entries expire (60s). That window is the worst-case revocation
delay across a fleet.
