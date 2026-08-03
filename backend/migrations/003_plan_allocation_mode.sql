ALTER TABLE shared_plans
    ADD COLUMN allocation_mode TEXT NOT NULL DEFAULT 'fixed'
    CHECK (allocation_mode IN ('fixed', 'shared'));

ALTER TABLE shared_plans
    ADD CONSTRAINT shared_plans_shared_public_share_check
    CHECK (allocation_mode = 'fixed' OR public_share_basis_points = 0);

ALTER TABLE plan_members
    DROP CONSTRAINT plan_members_share_basis_points_check,
    ADD CONSTRAINT plan_members_share_basis_points_check
    CHECK (share_basis_points BETWEEN 0 AND 10000);

ALTER TABLE plan_invites
    DROP CONSTRAINT plan_invites_share_basis_points_check,
    ADD CONSTRAINT plan_invites_share_basis_points_check
    CHECK (share_basis_points BETWEEN 0 AND 10000);
