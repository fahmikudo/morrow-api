-- +goose Up
create table attachments (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  entity_type text not null check (entity_type in ('project', 'task', 'note')),
  entity_id text not null,
  filename text not null,
  content_type text not null,
  byte_size bigint not null default 0 check (byte_size >= 0),
  storage_key text not null unique,
  status text not null default 'pending' check (status in ('pending', 'ready')),
  created_at timestamptz not null default now()
);
create index attachments_entity_idx
  on attachments (user_id, entity_type, entity_id, created_at desc);
create index attachments_pending_idx
  on attachments (user_id, status, created_at);

-- +goose Down
drop table if exists attachments;
