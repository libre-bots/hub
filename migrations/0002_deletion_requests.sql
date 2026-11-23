-- +goose Up
CREATE TABLE deletion_requests (
    bot          TEXT        NOT NULL,
    user_id      BIGINT      NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (bot, user_id)
);

-- +goose Down
DROP TABLE deletion_requests;
