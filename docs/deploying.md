# Deploying

The API needs two things at runtime: a Bible corpus (`data/bible.db`) and a
Supabase Postgres connection. How the corpus gets there is the only real
deployment decision.

## Two paths

| | Docker | Native Go buildpack |
| --- | --- | --- |
| Corpus built | Once, at image build | On every instance's first boot |
| Cold start | Immediate | Plus corpus build time |
| Runtime image | ~90MB corpus + binary | Whole repo, incl. ~225MB of seed sources |
| Risk | None once built | Slow instances can exceed the platform's port-detection timeout |

**Prefer Docker.** The native path exists so a source-based deploy is not
broken, not because it is the better arrangement.

The corpus build takes ~10s on a developer laptop but can take **minutes** on a
small shared-CPU instance. Since the server builds it before binding its port,
a platform that waits ~60s for a port to open may kill the service first. The
symptom is confusing: the logs show the app apparently starting fine, then
`no open ports detected`.

---

## Render

### Switching an existing service to Docker

1. Dashboard → your service → **Settings** → **Build & Deploy**.
2. Change **Runtime** (labelled *Language* on some accounts) to **Docker**.
3. Set **Dockerfile Path** to `./Dockerfile`.
4. Clear the Build Command and Start Command — with Docker, the image's
   `ENTRYPOINT` runs and those fields are ignored.
5. **Manual Deploy → Clear build cache & deploy.**

If Runtime is not editable on your plan or service age, create a new Web
Service with runtime Docker pointed at the same repo, confirm it is healthy,
then move the custom domain across and delete the old one. A committed
`render.yaml` (see repo root) also pins the runtime, so a Blueprint deploy
cannot drift back to a buildpack.

### Environment variables

Set in the dashboard, whichever runtime you use:

| Var | Value |
| --- | --- |
| `DATABASE_URL` | Supabase **session pooler** string, password URL-encoded |
| `SUPABASE_URL` | `https://<project-ref>.supabase.co` |
| `SCHOLIA_CORS_ORIGINS` | Your frontend origin(s), comma-separated |

Do **not** set `PORT` — Render injects it and the server reads it.

Do **not** set `SCHOLIA_DB_PATH` on the native runtime. `/app/data/bible.db` is
the in-image path and points nowhere outside the container; the default already
resolves `./data/bible.db` correctly.

No Supabase API key is required. The server only verifies tokens, using public
keys from the JWKS endpoint.

### Staying on the native Go runtime

It works, but the build command must name the package explicitly:

```
Build Command:  go build -tags netgo -ldflags '-s -w' -o app ./cmd/api
Start Command:  ./app
```

Without the trailing `./cmd/api`, `go build` looks for a main package in the
repo root, where there are no Go files at all.

Expect a slow first boot on each new instance while the corpus builds, and see
the port-detection warning above.

---

## Common failures

**`no open ports detected`** — the server bound a port the platform was not
watching, or took too long to bind. The server reads `PORT` and falls back to
8080; make sure nothing overrides it, and prefer Docker if seeding is slow.

**`bible database not found at ./data/bible.db`** — the corpus was neither
built into the image nor buildable at boot. On Docker, the build stage did not
run (wrong runtime selected). On the native runtime, check `SCHOLIA_AUTO_SEED`
is not set to false.

**`connect to postgres: ... network is unreachable`** — the DSN uses
`db.<ref>.supabase.co`, which has no A record and is reachable over IPv6 only.
Render cannot route to it. Switch to the session pooler:

```
postgresql://postgres.<project-ref>:<url-encoded-pw>@aws-0-<region>.pooler.supabase.com:5432/postgres
```

Two details that bite: the username becomes `postgres.<project-ref>` rather than
plain `postgres`, and the region in the hostname is the *database's* region,
which need not match where you host the API. If you do not know it, try
connecting to a few — only the correct one accepts the credentials.

**`DATABASE_URL points at the Supabase transaction pooler (:6543)`** — append
`?default_query_exec_mode=simple_protocol`, or use the session pooler instead.

**401 on every authenticated request** — the Supabase project is still using
the legacy shared HS256 secret. Migrate to asymmetric JWT signing keys
(Authentication → JWT Keys); there is no public key to verify against
otherwise.
