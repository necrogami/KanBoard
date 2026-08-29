-- name: InsertEvent :exec
INSERT INTO event (id, workspace_id, project_id, board_id, card_id, seq, actor_user_id, via_token_id, actor_kind, kind, payload, occurred_at)
VALUES (@id, @workspace_id, @project_id, @board_id, @card_id, @seq, @actor_user_id, @via_token_id, @actor_kind, @kind, @payload, @occurred_at);

-- name: ListEventsSince :many
SELECT * FROM event WHERE workspace_id = @workspace_id AND seq > @seq ORDER BY seq LIMIT CAST(@lim AS BIGINT);

-- name: ListEventsByCard :many
SELECT * FROM event WHERE card_id = @card_id ORDER BY seq DESC LIMIT CAST(@lim AS BIGINT);
