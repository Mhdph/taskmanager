CREATE TABLE member_capacities (
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    daily_capacity_seconds INTEGER NOT NULL CHECK (daily_capacity_seconds BETWEEN 0 AND 86400),
    working_days SMALLINT[] NOT NULL CHECK (working_days <@ ARRAY[1,2,3,4,5,6,7]::smallint[] AND cardinality(working_days) <= 7),
    timezone TEXT NOT NULL CHECK (char_length(timezone) BETWEEN 1 AND 100),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES tenant_members (tenant_id, user_id) ON DELETE CASCADE
);
