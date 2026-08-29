-- name: CreateUser :one
INSERT INTO app_user (id, workspace_id, email, name, kind, password_hash, email_verified_at, locale, created_at, updated_at)
VALUES (@id, @workspace_id, @email, @name, @kind, @password_hash, @email_verified_at, @locale, @created_at, @updated_at)
RETURNING *;

-- name: GetUser :one
SELECT * FROM app_user WHERE id = @id;

-- name: GetUserByEmail :one
SELECT * FROM app_user WHERE workspace_id = @workspace_id AND email = @email;

-- name: UpsertWorkspaceMember :exec
INSERT INTO workspace_member (workspace_id, user_id, role, created_at)
VALUES (@workspace_id, @user_id, @role, @created_at)
ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = excluded.role;

-- name: GetWorkspaceMember :one
SELECT * FROM workspace_member WHERE workspace_id = @workspace_id AND user_id = @user_id;
