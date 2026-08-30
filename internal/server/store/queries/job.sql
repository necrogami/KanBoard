-- name: InsertJob :exec
INSERT INTO job (id, workspace_id, kind, payload, state, attempts, max_attempts, run_at, created_at)
VALUES (@id, @workspace_id, @kind, @payload, 'queued', 0, @max_attempts, @run_at, @created_at);

-- name: LeaseJobs :many
UPDATE job SET state = 'leased', lease_owner = @owner, lease_expires_at = @lease_until, attempts = attempts + 1
WHERE id IN (
    SELECT id FROM job AS j
    WHERE (j.state = 'queued' AND j.run_at <= @now_queued) OR (j.state = 'leased' AND j.lease_expires_at < @now_leased)
    ORDER BY j.run_at, j.id
    LIMIT CAST(@batch AS BIGINT)
)
RETURNING *;

-- name: CompleteJob :exec
-- CompleteJob releases the lease only while this owner still holds it. A
-- runner whose lease expired, and whose job another runner has already
-- reclaimed, must not clobber the new owner's outcome: the update matches
-- no row instead. RetryJob and DeadJob carry the same guard.
UPDATE job SET state = 'done', completed_at = @completed_at, lease_owner = NULL, lease_expires_at = NULL
WHERE id = @id AND state = 'leased' AND lease_owner = @owner;

-- name: RetryJob :exec
-- RetryJob is guarded by the lease owner; see CompleteJob.
UPDATE job SET state = 'queued', run_at = @run_at, last_error = @last_error, lease_owner = NULL, lease_expires_at = NULL
WHERE id = @id AND state = 'leased' AND lease_owner = @owner;

-- name: DeadJob :exec
-- DeadJob is guarded by the lease owner; see CompleteJob.
UPDATE job SET state = 'dead', completed_at = @completed_at, last_error = @last_error, lease_owner = NULL, lease_expires_at = NULL
WHERE id = @id AND state = 'leased' AND lease_owner = @owner;

-- name: GetJob :one
SELECT * FROM job WHERE id = @id;

-- name: ListJobsByState :many
SELECT * FROM job WHERE state = @state ORDER BY created_at LIMIT CAST(@lim AS BIGINT);
