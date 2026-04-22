-- name: CreateCLIDeviceCode :one
INSERT INTO cli_device_code (device_code, user_code, hostname, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetCLIDeviceCodeByUserCode :one
SELECT * FROM cli_device_code
WHERE user_code = $1;

-- name: GetCLIDeviceCodeByDeviceCode :one
SELECT * FROM cli_device_code
WHERE device_code = $1;

-- name: ApproveCLIDeviceCode :one
UPDATE cli_device_code
SET status      = 'approved',
    user_id     = $2,
    token       = $3,
    approved_at = now()
WHERE user_code = $1
  AND status = 'pending'
  AND expires_at > now()
RETURNING *;

-- name: DenyCLIDeviceCode :exec
UPDATE cli_device_code
SET status = 'denied'
WHERE user_code = $1
  AND status = 'pending';

-- name: DeleteExpiredCLIDeviceCodes :exec
DELETE FROM cli_device_code
WHERE expires_at < now() - INTERVAL '1 hour';
