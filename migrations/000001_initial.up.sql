create table users (
  id text primary key,
  email text not null unique,
  password_hash text not null,
  created_at timestamptz not null
);

create table sessions (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  token_hash text not null unique,
  created_at timestamptz not null,
  expires_at timestamptz not null
);
create index sessions_user_idx on sessions(user_id);

create table profile (
  user_id text primary key references users(id) on delete cascade,
  name text not null,
  email text not null,
  role text,
  timezone text,
  joined_at timestamptz not null,
  updated_at timestamptz not null
);

create table areas (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  name text not null,
  description text,
  tone text not null check (tone in ('accent','success','warning','danger','neutral')),
  icon text not null check (icon in ('briefcase','users','heart','user','book','home','sparkles')),
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create table projects (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  "key" text not null,
  name text not null,
  description text,
  status text not null check (status in ('active','completed','archived')),
  area_id text references areas(id) on delete set null,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (user_id, "key")
);

create table tasks (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  "key" text not null,
  title text not null,
  description text,
  type text not null check (type in ('task','story','bug')),
  status text not null check (status in ('todo','in_progress','done')),
  priority text not null check (priority in ('low','medium','high')),
  due_date date,
  start_time time,
  end_time time,
  project_id text references projects(id) on delete set null,
  area_id text references areas(id) on delete set null,
  parent_id text references tasks(id) on delete set null,
  labels text[] not null default '{}',
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (user_id, "key")
);

create table notes (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  title text not null,
  content text not null,
  tags text[] not null default '{}',
  project_id text references projects(id) on delete set null,
  area_id text references areas(id) on delete set null,
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create table events (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  title text not null,
  date date not null,
  start_time time,
  end_time time,
  notes text,
  project_id text references projects(id) on delete set null,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  source text not null default 'local' check (source in ('local', 'google'))
);

create table habits (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  title text not null,
  description text,
  area_id text references areas(id) on delete set null,
  project_id text references projects(id) on delete set null,
  cadence text not null check (cadence in ('daily','weekly','monthly')),
  target int not null check (target >= 1),
  schedule_kind text not null check (schedule_kind in ('flexible','fixed')),
  weekdays int[],
  start_time time,
  end_time time,
  start_date date not null,
  archived_at timestamptz,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  check (
    (schedule_kind = 'flexible' and weekdays is null)
    or (schedule_kind = 'fixed' and weekdays is not null
        and cardinality(weekdays) >= 1
        and weekdays <@ array[0,1,2,3,4,5,6])
  )
);

create table habit_check_ins (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  habit_id text not null references habits(id) on delete cascade,
  date date not null,
  created_at timestamptz not null,
  unique (habit_id, date)
);

create table inbox_items (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  title text not null,
  content text,
  type text not null check (type in ('note','task','idea')),
  created_at timestamptz not null,
  archived_at timestamptz
);

create table counters (
  user_id text not null references users(id) on delete cascade,
  bucket text not null,
  next int not null default 0 check (next >= 0),
  primary key (user_id, bucket)
);

create table integrations (
  user_id text not null references users(id) on delete cascade,
  integration_id text not null,
  enabled boolean not null default true,
  connected boolean not null default false,
  last_sync_at timestamptz,
  account_email text,
  calendar_id text,
  last_error text,
  primary key (user_id, integration_id)
);

create table ai_settings (
  user_id text primary key references users(id) on delete cascade,
  enabled boolean not null default false,
  provider text not null default 'deepseek' check (provider = 'deepseek'),
  model text not null default 'deepseek-flash',
  reasoning_effort text check (reasoning_effort in ('none','low','high','max')),
  status text not null default 'untested' check (status in ('untested','ok','error')),
  last_tested_at timestamptz,
  api_key text
);

create table api_tokens (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  name text not null,
  token_hash text not null unique,
  created_at timestamptz not null,
  last_used_at timestamptz
);
create index api_tokens_user_idx on api_tokens(user_id);

create table reminder_settings (
  user_id text primary key references users(id) on delete cascade,
  enabled boolean not null default true,
  task_time time not null default '09:00',
  event_lead_minutes int not null default 15 check (event_lead_minutes between 0 and 1440),
  updated_at timestamptz not null default now()
);

create table push_subscriptions (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  endpoint text not null unique,
  p256dh text not null,
  auth text not null,
  user_agent text,
  created_at timestamptz not null,
  last_used_at timestamptz
);
create index push_subscriptions_user_idx on push_subscriptions(user_id);

create table reminder_deliveries (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  kind text not null check (kind in ('task','event','habit')),
  ref_id text not null,
  occurrence_date date not null,
  title text not null,
  body text not null,
  href text not null,
  sent_at timestamptz not null,
  dismissed_at timestamptz,
  unique (user_id, kind, ref_id, occurrence_date)
);
create index reminder_deliveries_user_date_idx on reminder_deliveries(user_id, occurrence_date);

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

create table task_external_links (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  task_id text references tasks(id) on delete set null,
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

create index tasks_user_idx on tasks(user_id);
create index tasks_project_idx on tasks(project_id);
create index tasks_parent_idx on tasks(parent_id);
create index notes_user_idx on notes(user_id);
create index events_user_date_idx on events(user_id, date);
create index habits_user_idx on habits(user_id);
create index habit_check_ins_habit_idx on habit_check_ins(habit_id);
create index inbox_items_user_archived_idx on inbox_items(user_id, archived_at);
create index projects_user_idx on projects(user_id);
