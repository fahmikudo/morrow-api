-- +goose Up
-- 0005 made task_id globally unique, which would block a second provider for
-- the same task. The composite unique(task_id, integration_id) is the intended
-- constraint.
alter table task_external_links
  drop constraint if exists task_external_links_task_id_key;

-- +goose Down
alter table task_external_links
  add constraint task_external_links_task_id_key unique (task_id);
