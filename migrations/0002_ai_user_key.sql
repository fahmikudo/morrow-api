-- +goose Up
alter table ai_settings add column api_key text;

-- +goose Down
alter table ai_settings drop column if exists api_key;
