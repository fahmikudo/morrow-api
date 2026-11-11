alter table habits
  add column end_date date,
  add constraint habits_end_date_check
    check (end_date is null or end_date >= start_date);
