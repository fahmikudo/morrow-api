-- +goose Up
alter table events add column source text not null default 'local'
  check (source in ('local', 'google'));

alter table integrations
  add column account_email text,
  add column calendar_id text,
  add column last_error text;

create table integration_credentials (
  user_id text not null references users(id) on delete cascade,
  integration_id text not null,
  access_token_enc text not null,
  refresh_token_enc text,
  token_expiry timestamptz,
  scope text,
  sync_token text,
  last_sync_at timestamptz,
  updated_at timestamptz not null,
  primary key (user_id, integration_id)
);

create table oauth_states (
  state_hash text primary key,
  user_id text not null references users(id) on delete cascade,
  code_verifier_enc text not null,
  expires_at timestamptz not null
);
create index oauth_states_user_idx on oauth_states(user_id);

create table calendar_event_links (
  id text primary key,
  event_id text unique references events(id) on delete set null,
  user_id text not null references users(id) on delete cascade,
  integration_id text not null,
  calendar_id text not null,
  external_id text not null,
  external_etag text,
  external_updated_at timestamptz,
  unique (user_id, integration_id, calendar_id, external_id)
);
create index calendar_event_links_external_idx
  on calendar_event_links(user_id, integration_id, calendar_id, external_id);

-- +goose Down
drop table if exists calendar_event_links;
drop table if exists oauth_states;
drop table if exists integration_credentials;
alter table integrations
  drop column if exists last_error,
  drop column if exists calendar_id,
  drop column if exists account_email;
alter table events drop column if exists source;
