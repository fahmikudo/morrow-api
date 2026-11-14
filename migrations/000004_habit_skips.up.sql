create table habit_skips (
  id text primary key,
  user_id text not null references users(id) on delete cascade,
  habit_id text not null references habits(id) on delete cascade,
  date date not null,
  created_at timestamptz not null,
  unique (habit_id, date)
);
