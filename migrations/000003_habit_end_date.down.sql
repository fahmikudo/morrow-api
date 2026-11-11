alter table habits
  drop constraint habits_end_date_check,
  drop column end_date;
