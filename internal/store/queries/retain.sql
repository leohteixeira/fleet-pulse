-- name: DeleteFinishedCommandsOlderThan :execrows
DELETE FROM commands
WHERE updated_at < $1
  AND state IN ('ACKED', 'FAILED', 'TIMEOUT', 'CANCELLED');

-- name: DeleteAuditOlderThan :execrows
DELETE FROM audit_log
WHERE created_at < $1;
