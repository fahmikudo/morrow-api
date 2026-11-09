-- +goose Up
delete from integrations
where integration_id not in ('google-calendar', 'softwareseni-pepy');

-- +goose Down
-- Removed integrations are not restored; reconnecting is a deliberate choice.
