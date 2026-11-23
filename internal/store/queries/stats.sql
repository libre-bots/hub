-- name: FiguresPerBot :many
SELECT bot, count(*)::bigint AS guilds, coalesce(sum(member_count), 0)::bigint AS members
FROM guilds
WHERE left_at IS NULL AND bot = ANY (sqlc.arg(bots)::text[])
GROUP BY bot;

-- name: FiguresTotal :one
SELECT count(*)::bigint AS guilds, coalesce(sum(member_count), 0)::bigint AS members
FROM (
    SELECT DISTINCT ON (guild_id) member_count
    FROM guilds
    WHERE left_at IS NULL AND bot = ANY (sqlc.arg(bots)::text[])
    ORDER BY guild_id, counted_at DESC, member_count DESC
) latest;

-- name: UpsertStatsDaily :exec
INSERT INTO stats_daily (day, scope, guild_count, member_count)
VALUES (sqlc.arg(day)::date, sqlc.arg(scope), sqlc.arg(guild_count), sqlc.arg(member_count))
ON CONFLICT (day, scope) DO UPDATE SET
    guild_count = EXCLUDED.guild_count,
    member_count = EXCLUDED.member_count;
