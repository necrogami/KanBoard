-- name: CreateLabel :one
INSERT INTO label (id, workspace_id, project_id, name, color, kind, created_at, updated_at)
VALUES (@id, @workspace_id, @project_id, @name, @color, 'label', @created_at, @updated_at)
RETURNING *;

-- name: GetLabel :one
SELECT * FROM label WHERE id = @id;

-- name: ListLabels :many
SELECT * FROM label WHERE project_id = @project_id ORDER BY name;

-- name: ListCardLabelIDs :many
SELECT label_id FROM card_label WHERE card_id = @card_id;

-- name: ListCardLabelIDsByBoard :many
-- ListCardLabelIDsByBoard is the batched form of ListCardLabelIDs for a
-- whole board: one query instead of one per card, grouped by the caller.
SELECT cl.card_id, cl.label_id FROM card_label AS cl
JOIN card AS c ON c.id = cl.card_id
WHERE c.board_id = @board_id AND c.archived_at IS NULL;

-- name: AddCardLabel :exec
INSERT INTO card_label (workspace_id, card_id, label_id) VALUES (@workspace_id, @card_id, @label_id) ON CONFLICT DO NOTHING;

-- name: RemoveCardLabel :exec
DELETE FROM card_label WHERE card_id = @card_id AND label_id = @label_id;

-- name: ListCardAssigneeIDs :many
SELECT user_id FROM card_assignee WHERE card_id = @card_id;

-- name: ListCardAssigneeIDsByBoard :many
-- ListCardAssigneeIDsByBoard is the batched form of ListCardAssigneeIDs
-- for a whole board; see ListCardLabelIDsByBoard.
SELECT ca.card_id, ca.user_id FROM card_assignee AS ca
JOIN card AS c ON c.id = ca.card_id
WHERE c.board_id = @board_id AND c.archived_at IS NULL;

-- name: AddCardAssignee :exec
INSERT INTO card_assignee (workspace_id, card_id, user_id) VALUES (@workspace_id, @card_id, @user_id) ON CONFLICT DO NOTHING;

-- name: RemoveCardAssignee :exec
DELETE FROM card_assignee WHERE card_id = @card_id AND user_id = @user_id;
