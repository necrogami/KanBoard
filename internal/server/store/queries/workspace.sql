-- name: CreateWorkspace :one
INSERT INTO workspace (id, name, slug, next_seq, created_at, updated_at)
VALUES (@id, @name, @slug, 0, @created_at, @updated_at)
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM workspace WHERE id = @id;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspace WHERE slug = @slug;

-- name: CountWorkspaces :one
SELECT count(*) FROM workspace;

-- name: NextSeq :one
UPDATE workspace SET next_seq = next_seq + 1 WHERE id = @id RETURNING next_seq;
