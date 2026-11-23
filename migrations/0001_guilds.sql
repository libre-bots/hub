-- +goose Up
CREATE TABLE guilds (
    bot          TEXT        NOT NULL,
    guild_id     BIGINT      NOT NULL,
    locale       TEXT        NOT NULL,
    member_count INTEGER     NOT NULL,
    counted_at   TIMESTAMPTZ NOT NULL,
    joined_at    TIMESTAMPTZ NOT NULL,
    left_at      TIMESTAMPTZ,
    PRIMARY KEY (bot, guild_id)
);
CREATE INDEX guilds_left_at_idx ON guilds (left_at) WHERE left_at IS NOT NULL;

-- +goose Down
DROP TABLE guilds;
