-- name: ListDueGuilds :many
SELECT bot, guild_id FROM guilds
WHERE left_at < sqlc.arg(cutoff)::timestamptz
ORDER BY bot, guild_id;

-- name: LockDueGuild :one
SELECT guild_id FROM guilds
WHERE bot = sqlc.arg(bot) AND guild_id = sqlc.arg(guild_id) AND left_at < sqlc.arg(cutoff)::timestamptz
FOR UPDATE;

-- name: DeleteGuildRow :exec
DELETE FROM guilds WHERE bot = sqlc.arg(bot) AND guild_id = sqlc.arg(guild_id);
