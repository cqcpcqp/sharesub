ALTER TABLE shared_plans
    ADD COLUMN description TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT shared_plans_description_check CHECK (length(description) <= 2000);
