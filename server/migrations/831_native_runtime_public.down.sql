-- Reverse 831: back to the 826 defaults (private, ownerless).
UPDATE agent_runtime
SET visibility = 'private', owner_id = NULL
WHERE runtime_mode = 'native' AND daemon_id = 'native';
