-- name: CreateBoard :one
INSERT INTO board (id, workspace_id, project_id, name, position, version, created_at, updated_at)
VALUES (@id, @workspace_id, @project_id, @name, @position, 1, @created_at, @updated_at)
RETURNING *;

-- name: GetBoard :one
SELECT * FROM board WHERE id = @id;

-- name: GetBoardByProject :one
SELECT * FROM board WHERE project_id = @project_id AND archived_at IS NULL ORDER BY position LIMIT 1;

-- name: CreateColumn :one
INSERT INTO board_column (id, workspace_id, board_id, name, position, category, version, created_at, updated_at)
VALUES (@id, @workspace_id, @board_id, @name, @position, @category, 1, @created_at, @updated_at)
RETURNING *;

-- name: GetColumn :one
SELECT * FROM board_column WHERE id = @id;

-- name: ListColumns :many
SELECT * FROM board_column WHERE board_id = @board_id AND archived_at IS NULL ORDER BY position;
