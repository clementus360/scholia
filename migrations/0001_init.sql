-- Scholia user data schema (Supabase Postgres).
--
-- Everything a person creates lives here. The Bible corpus stays in a read-only
-- SQLite file baked into the container image, so it can be rebuilt freely
-- without touching accounts or notes.
--
-- Identity is owned by Supabase Auth (auth.users). This schema stores only the
-- application-specific parts: roles, invite gating, API keys, and notes.
--
-- Apply with:
--   psql "$DATABASE_URL" -f migrations/0001_init.sql

begin;

-- ---------------------------------------------------------------------------
-- profiles: application data hanging off a Supabase Auth user.
-- ---------------------------------------------------------------------------
-- Deliberately not named "users": auth.users is the source of truth for
-- identity, and shadowing it invites writing to the wrong one. Deleting the
-- auth user cascades the profile away with it.
create table if not exists public.profiles (
    id           uuid primary key references auth.users (id) on delete cascade,
    display_name text,
    role         text        not null default 'member',
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    constraint profiles_role_valid check (role in ('member', 'admin'))
);

-- Admin is a role on the profile, replacing the old SCHOLIA_ADMIN_SUBJECT /
-- SCHOLIA_ADMIN_USER_ID environment matching. Promote a user with:
--   update public.profiles set role = 'admin' where id = '<uuid>';
create index if not exists idx_profiles_role on public.profiles (role) where role = 'admin';

-- (An invite_codes table lived here when access was invite-gated. Sign-up is
-- open now — see 0002_open_signup.sql — so it is no longer created.)

-- ---------------------------------------------------------------------------
-- api_keys: long-lived credentials for programmatic clients.
-- ---------------------------------------------------------------------------
-- Browser sessions use Supabase JWTs and never touch this table. It exists for
-- scripts and integrations that cannot perform a refresh-token dance.
create table if not exists public.api_keys (
    id           uuid        primary key default gen_random_uuid(),
    user_id      uuid        not null references auth.users (id) on delete cascade,
    token_hash   text        not null unique,
    label        text,
    scopes       text[]      not null default array['read'],
    active       boolean     not null default true,
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now()
);

-- Authentication looks keys up by hash on every request that uses one, and only
-- ever cares about active keys.
create index if not exists idx_api_keys_token_hash on public.api_keys (token_hash) where active;
create index if not exists idx_api_keys_user_id on public.api_keys (user_id);

-- ---------------------------------------------------------------------------
-- notes: user-authored study notes.
-- ---------------------------------------------------------------------------
create table if not exists public.notes (
    id             bigint      generated always as identity primary key,
    owner_user_id  uuid        not null references auth.users (id) on delete cascade,
    title          text        not null default '',
    main_reference text        not null default '',
    content        text        not null default '',
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now()
);

-- Every notes query filters by owner, and the list endpoints order by
-- updated_at desc. This index serves both.
create index if not exists idx_notes_owner_updated on public.notes (owner_user_id, updated_at desc, id desc);

-- ---------------------------------------------------------------------------
-- note_verses: which verses a note is attached to.
-- ---------------------------------------------------------------------------
-- verse_id is an OSIS reference such as 'GEN.1.1'. It intentionally has NO
-- foreign key: the verses table lives in a different database entirely (the
-- read-only SQLite corpus). References are validated in Go against the Bible
-- handle before insert, via storage.ExpandVerseReferences.
create table if not exists public.note_verses (
    note_id  bigint not null references public.notes (id) on delete cascade,
    verse_id text   not null,
    primary key (note_id, verse_id)
);

-- Looking up "notes attached to this verse" is the hot path on verse pages.
create index if not exists idx_note_verses_verse on public.note_verses (verse_id);

-- ---------------------------------------------------------------------------
-- updated_at maintenance
-- ---------------------------------------------------------------------------
create or replace function public.touch_updated_at()
returns trigger
language plpgsql
as $$
begin
    new.updated_at := now();
    return new;
end;
$$;

drop trigger if exists notes_touch_updated_at on public.notes;
create trigger notes_touch_updated_at
    before update on public.notes
    for each row execute function public.touch_updated_at();

drop trigger if exists profiles_touch_updated_at on public.profiles;
create trigger profiles_touch_updated_at
    before update on public.profiles
    for each row execute function public.touch_updated_at();

-- ---------------------------------------------------------------------------
-- Row Level Security
-- ---------------------------------------------------------------------------
-- The Go API connects as the database owner and enforces ownership itself:
-- every notes query already filters on owner_user_id. RLS is enabled anyway as
-- a backstop, so that if a PostgREST/anon key path is ever opened up, these
-- tables are not readable by default.
--
-- No permissive policy is created for api_keys: it holds credential hashes and
-- should never be reachable from a client key.
alter table public.profiles    enable row level security;
alter table public.notes       enable row level security;
alter table public.note_verses enable row level security;
alter table public.api_keys    enable row level security;

drop policy if exists profiles_self_select on public.profiles;
create policy profiles_self_select on public.profiles
    for select using ((select auth.uid()) = id);

drop policy if exists notes_owner_all on public.notes;
create policy notes_owner_all on public.notes
    for all using ((select auth.uid()) = owner_user_id)
    with check ((select auth.uid()) = owner_user_id);

drop policy if exists note_verses_owner_all on public.note_verses;
create policy note_verses_owner_all on public.note_verses
    for all using (
        exists (
            select 1 from public.notes n
            where n.id = note_verses.note_id
              and n.owner_user_id = (select auth.uid())
        )
    );

-- ---------------------------------------------------------------------------
-- Auto-create a profile whenever a Supabase Auth user is created.
-- ---------------------------------------------------------------------------
-- Without this, a user signing up through any Supabase path (including the
-- dashboard) would have no profile and therefore no role.
create or replace function public.handle_new_user()
returns trigger
language plpgsql
security definer
set search_path = public
as $$
begin
    insert into public.profiles (id, display_name)
    values (
        new.id,
        coalesce(new.raw_user_meta_data ->> 'display_name', split_part(new.email, '@', 1))
    )
    on conflict (id) do nothing;
    return new;
end;
$$;

drop trigger if exists on_auth_user_created on auth.users;
create trigger on_auth_user_created
    after insert on auth.users
    for each row execute function public.handle_new_user();

commit;
