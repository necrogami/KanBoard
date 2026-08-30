-- name: CreateProject :one
INSERT INTO project (id, workspace_id, key, name, next_card_number, version, created_at, updated_at)
VALUES (@id, @workspace_id, @key, @name, 0, 1, @created_at, @updated_at)
RETURNING *;

-- name: GetProject :one
SELECT * FROM project WHERE id = @id AND workspace_id = @workspace_id;

-- name: GetProjectByKey :one
SELECT * FROM project WHERE workspace_id = @workspace_id AND key = @key;

-- name: ListProjects :many
SELECT * FROM project WHERE workspace_id = @workspace_id AND archived_at IS NULL ORDER BY key;

-- name: NextCardNumber :one
UPDATE project SET next_card_number = next_card_number + 1, updated_at = @updated_at
WHERE id = @id AND workspace_id = @workspace_id RETURNING next_card_number;

-- name: UpsertProjectMember :exec
INSERT INTO project_member (workspace_id, project_id, user_id, role, created_at)
VALUES (@workspace_id, @project_id, @user_id, @role, @created_at)
ON CONFLICT (project_id, user_id) DO UPDATE SET role = excluded.role;

-- name: ListProjectMembershipsForUser :many
SELECT * FROM project_member WHERE user_id = @user_id AND workspace_id = @workspace_id;

-- name: ListProjectMembers :many
SELECT * FROM project_member WHERE project_id = @project_id AND workspace_id = @workspace_id;
