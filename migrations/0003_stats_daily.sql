-- +goose Up
CREATE TABLE stats_daily (
    day          DATE   NOT NULL,
    scope        TEXT   NOT NULL,
    guild_count  BIGINT NOT NULL,
    member_count BIGINT NOT NULL,
    PRIMARY KEY (day, scope)
);

-- +goose Down
DROP TABLE stats_daily;
