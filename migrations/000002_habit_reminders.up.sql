alter table habits
  add column reminder_enabled boolean,
  add column reminder_lead_minutes int
    check (reminder_lead_minutes between 0 and 1440);
