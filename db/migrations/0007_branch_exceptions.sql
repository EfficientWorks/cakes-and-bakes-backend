-- +goose Up

-- Exception dates for holidays, private events and closures (SCH-07).
-- is_closed = true with open_time/close_time NULL means closed all day;
-- otherwise open_time/close_time override the normal weekly hours for
-- that one date.
CREATE TABLE branch_exceptions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    date        DATE NOT NULL,
    reason      TEXT NOT NULL,
    is_closed   BOOLEAN NOT NULL DEFAULT true,
    open_time   TIME,
    close_time  TIME,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (branch_id, date)
);

CREATE INDEX idx_branch_exceptions_branch_date ON branch_exceptions (branch_id, date);

-- +goose Down
DROP TABLE IF EXISTS branch_exceptions;
