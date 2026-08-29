-- name: CreateCard :one
INSERT INTO card (id, workspace_id, project_id, board_id, column_id, number, title, description, position, due_date, created_by, version, created_at, updated_at)
VALUES (@id, @workspace_id, @project_id, @board_id, @column_id, @number, @title, @description, @position, @due_date, @created_by, 1, @created_at, @updated_at)
RETURNING *;

-- name: GetCard :one
SELECT * FROM card WHERE id = @id;

-- name: GetCardByNumber :one
SELECT * FROM card WHERE project_id = @project_id AND number = @number;

-- name: ListCardsByBoard :many
SELECT * FROM card WHERE board_id = @board_id AND archived_at IS NULL ORDER BY column_id, position;

-- name: ListCardsByColumn :many
SELECT * FROM card WHERE column_id = @column_id AND archived_at IS NULL ORDER BY position;

-- name: FirstPositionInColumn :one
SELECT position FROM card WHERE column_id = @column_id ORDER BY position ASC LIMIT 1;

-- name: LastPositionInColumn :one
SELECT position FROM card WHERE column_id = @column_id ORDER BY position DESC LIMIT 1;

-- name: NextPositionAfter :one
SELECT position FROM card WHERE column_id = @column_id AND position > @position ORDER BY position ASC LIMIT 1;

-- name: PrevPositionBefore :one
SELECT position FROM card WHERE column_id = @column_id AND position < @position ORDER BY position DESC LIMIT 1;

-- Every versioned update carries "AND version = @version": on Postgres
-- READ COMMITTED two transactions can both read version 1, so the check
-- must be in the UPDATE itself; zero rows means E_CONFLICT.

-- name: MoveCard :one
UPDATE card SET column_id = @column_id, position = @position, completed_at = @completed_at, version = version + 1, updated_at = @updated_at
WHERE id = @id AND version = @version RETURNING *;

-- name: UpdateCardFields :one
UPDATE card SET title = @title, description = @description, due_date = @due_date, version = version + 1, updated_at = @updated_at
WHERE id = @id AND version = @version RETURNING *;

-- name: SetCardArchived :one
UPDATE card SET archived_at = @archived_at, version = version + 1, updated_at = @updated_at
WHERE id = @id AND version = @version RETURNING *;

-- name: TouchCard :one
UPDATE card SET version = version + 1, updated_at = @updated_at
WHERE id = @id AND version = @version RETURNING *;

-- name: CountCardsInColumn :one
SELECT count(*) FROM card WHERE column_id = @column_id AND archived_at IS NULL;
