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

-- name: UpdateJobLease :execrows
-- UpdateJobLease pushes one job's lease deadline out. LeaseJobs stamps a
-- single deadline for a whole batch that then runs serially, so a slow
-- job at the head would otherwise leave the jobs behind it holding a
-- deadline that has already passed by the time they run. Zero rows means
-- the lease is no longer ours and the job must not be run.
UPDATE job SET lease_expires_at = @lease_until
WHERE id = @id AND state = 'leased' AND lease_owner = @owner;

-- name: ReleaseJob :exec
-- ReleaseJob puts a leased job back in the queue exactly as it was found:
-- the same run_at and the same attempt count, undoing the increment
-- LeaseJobs made. The runner uses it when its context is cancelled while
-- a handler runs, so a shutdown neither burns an attempt nor leaves the
-- row leased for the rest of the lease. Guarded by the lease owner; see
-- CompleteJob.
UPDATE job SET state = 'queued', run_at = @run_at, attempts = @attempts, lease_owner = NULL, lease_expires_at = NULL
WHERE id = @id AND state = 'leased' AND lease_owner = @owner;

-- name: CountQueuedJobs :one
-- CountQueuedJobs reports how many jobs with this kind and payload are
-- still waiting to run. An enqueuer whose job is idempotent in its
-- payload calls it inside its own transaction to skip a duplicate.
SELECT count(*) FROM job WHERE kind = @kind AND payload = @payload AND state = 'queued';

-- name: GetJob :one
SELECT * FROM job WHERE id = @id;

-- name: ListJobsByState :many
SELECT * FROM job WHERE state = @state ORDER BY created_at LIMIT CAST(@lim AS BIGINT);
