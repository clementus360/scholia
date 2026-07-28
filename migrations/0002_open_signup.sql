-- Move from invite-gated access to open sign-up.
--
-- Identity is now entirely Supabase's job. Anyone who signs in with an enabled
-- provider (email, Google, magic link, ...) gets access; the API validates the
-- resulting access token and nothing more. The invite tables and the server's
-- Supabase admin credentials are no longer needed.
--
-- Apply with:
--   psql "$DATABASE_URL" -f migrations/0002_open_signup.sql

begin;

-- invite_codes was the account-creation gate. With open sign-up there is no
-- gate, so the table and its data serve no purpose.
--
-- If you later want to re-close access, do NOT restore this table as an
-- account-creation gate — Supabase would still let anyone create an account
-- directly through the publishable key, which ships in the frontend. Gate
-- *authorization* instead: add an approval column to profiles and check it
-- when building the principal.
drop table if exists public.invite_codes;

-- profiles.role is retained. Nothing enforces it today (there are no admin-only
-- endpoints left), but it costs nothing and is the right place to hang one.
comment on column public.profiles.role is
    'Reserved for future admin-only endpoints. Not enforced while access is open.';

comment on table public.api_keys is
    'Long-lived credentials for scripts that cannot run Supabase''s refresh cycle. '
    'Created and revoked by their owner via /api/v1/auth/api-keys, session-authenticated only.';

commit;
