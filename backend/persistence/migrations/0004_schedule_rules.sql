-- +goose Up
CREATE TABLE schedule_rules (
  id          INTEGER PRIMARY KEY,
  days_mask   INTEGER NOT NULL,    -- bit 0 = Sunday, bit 6 = Saturday
  start_min   INTEGER NOT NULL,    -- minutes since midnight (0..1439)
  end_min     INTEGER NOT NULL,
  down_kbps   INTEGER NOT NULL,    -- 0 = unlimited
  up_kbps     INTEGER NOT NULL,
  alt_only    INTEGER NOT NULL DEFAULT 0,  -- 1 = use alt-speed values, ignore down/up_kbps
  enabled     INTEGER NOT NULL DEFAULT 1
);

-- +goose Down
DROP TABLE schedule_rules;
