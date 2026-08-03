CREATE TEMP TABLE current_member_quota_windows ON COMMIT DROP AS
SELECT
    q.member_id,
    q.account_id,
    q.window_type,
    a.window_start,
    a.reset_at,
    least(100000000::bigint, sum(q.used_micros)) AS used_micros,
    a.used_micros AS account_used_micros,
    max(q.updated_at) AS updated_at
FROM member_quota_windows q
JOIN account_quota_snapshots a
    ON a.account_id = q.account_id
    AND a.window_type = q.window_type
    AND a.reset_at > now()
WHERE q.reset_at > now()
GROUP BY q.member_id, q.account_id, q.window_type, a.window_start, a.reset_at, a.used_micros;

DELETE FROM member_quota_windows;

ALTER TABLE member_quota_windows
    DROP CONSTRAINT member_quota_windows_pkey,
    ADD PRIMARY KEY (member_id, account_id, window_type);

INSERT INTO member_quota_windows(
    member_id,
    account_id,
    window_type,
    window_start,
    reset_at,
    used_micros,
    account_used_micros,
    updated_at
)
SELECT
    member_id,
    account_id,
    window_type,
    window_start,
    reset_at,
    used_micros,
    account_used_micros,
    updated_at
FROM current_member_quota_windows;
