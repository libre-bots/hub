-- name: RequestDeletion :exec
INSERT INTO deletion_requests (bot, user_id, requested_at)
VALUES (sqlc.arg(bot), sqlc.arg(user_id), sqlc.arg(now))
ON CONFLICT (bot, user_id) DO NOTHING;

-- name: ListPendingDeletions :many
SELECT user_id, requested_at FROM deletion_requests
WHERE bot = sqlc.arg(bot)
ORDER BY requested_at, user_id;

-- name: ClearDeletionRequest :exec
DELETE FROM deletion_requests WHERE bot = sqlc.arg(bot) AND user_id = sqlc.arg(user_id);
