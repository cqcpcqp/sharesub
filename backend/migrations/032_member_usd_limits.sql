ALTER TABLE plan_members ADD COLUMN usd_limit_micros bigint
    CHECK (usd_limit_micros > 0 AND usd_limit_micros <= 9007199254740991);
