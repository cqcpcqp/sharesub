DROP INDEX plan_invites_pending_email_idx;

ALTER TABLE plan_invites
    DROP COLUMN email;
