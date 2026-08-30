-- name: CreateComment :one
INSERT INTO comment (id, workspace_id, card_id, author_id, body, via_token_id, created_at, updated_at)
VALUES (@id, @workspace_id, @card_id, @author_id, @body, @via_token_id, @created_at, @updated_at)
RETURNING *;

-- name: ListComments :many
SELECT * FROM comment WHERE card_id = @card_id AND workspace_id = @workspace_id AND deleted_at IS NULL ORDER BY created_at;
