ALTER TABLE shared_plans
    ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE shared_plans
    ADD CONSTRAINT shared_plans_public_account_check
    CHECK (visibility <> 'public' OR account_id IS NOT NULL);
