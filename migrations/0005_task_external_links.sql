-- +goose Up
create table task_external_links (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  task_id text unique references tasks(id) on delete set null,
  integration_id text not null,
  external_id text,
  external_ref text,
  enabled boolean not null default true,
  last_synced_at timestamptz,
  last_error text,
  metadata jsonb not null default '{}'::jsonb,
  unique (task_id, integration_id)
);
create unique index task_external_links_external_idx
  on task_external_links (integration_id, external_id)
  where external_id is not null;
create index task_external_links_user_idx
  on task_external_links (user_id, integration_id);

-- +goose Down
drop table if exists task_external_links;
