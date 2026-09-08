ALTER TABLE agent_memory_evaluation
 ADD COLUMN execution_status TEXT NOT NULL DEFAULT 'imported' CHECK (execution_status IN ('imported','queued','running','completed','failed','cancelled')),
 ADD COLUMN execution_runtime_id UUID,
 ADD COLUMN execution_request_id UUID,
 ADD COLUMN execution_deadline TIMESTAMPTZ;
