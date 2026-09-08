ALTER TABLE project
    ADD COLUMN memory_rules JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(memory_rules) = 'array' AND jsonb_array_length(memory_rules) <= 20),
    ADD COLUMN memory_revision INTEGER NOT NULL DEFAULT 0 CHECK (memory_revision >= 0),
    ADD COLUMN memory_reviewed_by UUID,
    ADD COLUMN memory_reviewed_at TIMESTAMPTZ;
