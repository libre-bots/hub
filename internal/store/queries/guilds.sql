-- name: UpsertGuild :one
WITH prev AS (
    SELECT g.left_at FROM guilds g WHERE g.bot = sqlc.arg(bot) AND g.guild_id = sqlc.arg(guild_id)
), up AS (
    INSERT INTO guilds (bot, guild_id, locale, member_count, counted_at, joined_at)
    VALUES (sqlc.arg(bot), sqlc.arg(guild_id), sqlc.arg(locale), sqlc.arg(member_count), sqlc.arg(now), sqlc.arg(now))
    ON CONFLICT (bot, guild_id) DO UPDATE SET
        member_count = EXCLUDED.member_count,
        counted_at = EXCLUDED.counted_at,
        joined_at = CASE WHEN guilds.left_at IS NULL THEN guilds.joined_at ELSE EXCLUDED.joined_at END,
        left_at = NULL
    RETURNING guild_id, locale
)
SELECT up.guild_id, up.locale, NOT EXISTS (SELECT 1 FROM prev WHERE prev.left_at IS NULL) AS joined
FROM up;

-- name: LockGuildSync :exec
SELECT pg_advisory_xact_lock(hashtextextended('guild-sync:' || sqlc.arg(bot)::text, 0));

-- name: MarkGuildLeft :exec
UPDATE guilds SET left_at = sqlc.arg(now)::timestamptz
WHERE bot = sqlc.arg(bot) AND guild_id = sqlc.arg(guild_id) AND left_at IS NULL;

-- name: MarkMissingGuildsLeft :execrows
UPDATE guilds SET left_at = sqlc.arg(now)::timestamptz
WHERE bot = sqlc.arg(bot)
  AND left_at IS NULL
  AND (guild_id >> 22) % sqlc.arg(shard_count)::bigint = ANY (sqlc.arg(shard_ids)::bigint[])
  AND guild_id <> ALL (coalesce(sqlc.arg(sent)::bigint[], '{}'));

-- name: GetGuildSettings :one
SELECT guild_id, locale FROM guilds
WHERE bot = sqlc.arg(bot) AND guild_id = sqlc.arg(guild_id) AND left_at IS NULL;

-- name: UpdateGuildLocale :one
UPDATE guilds SET locale = sqlc.arg(locale)
WHERE bot = sqlc.arg(bot) AND guild_id = sqlc.arg(guild_id) AND left_at IS NULL
RETURNING guild_id, locale;
