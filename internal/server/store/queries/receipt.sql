-- name: GetReceipt :one
SELECT * FROM command_receipt WHERE idempotency_key = @idempotency_key AND actor_id = @actor_id;

-- name: InsertReceipt :exec
INSERT INTO command_receipt (workspace_id, idempotency_key, actor_id, command_kind, result, created_at)
VALUES (@workspace_id, @idempotency_key, @actor_id, @command_kind, @result, @created_at);

-- name: DeleteReceiptsBefore :execrows
DELETE FROM command_receipt WHERE created_at < @before;
