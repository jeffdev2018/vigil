-- F01 (JEF-5): the delegate — a second, optional actor pair naming the
-- assignee's partner on an issue.
--
-- The delegate is deliberately inert: it triggers no run and carries no
-- status. The assignee remains the only run trigger and the only status
-- carrier, so nothing here is read by the enqueue path (see
-- service/issue_trigger.go WillEnqueueRun, which keys only on assignee).
--
-- 'squad' is NOT allowed, unlike assignee_type. A squad is a routing object
-- whose work runs through its leader; delegating to one would name a target
-- that cannot be a partner and has no inbox to consume (the same reason
-- isAssignmentRecipientType excludes it).
--
-- No FOREIGN KEY, per the repository rule: the pair is validated in
-- application code (validateActorPair) and cleaned up there too.
--
-- NOT VALID keeps this an instant catalog change on a hot table; 753
-- validates separately. The column is nullable with no default, so adding it
-- does not rewrite the table.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS delegate_type TEXT;
ALTER TABLE issue ADD COLUMN IF NOT EXISTS delegate_id UUID;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_delegate_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_delegate_type_check
    CHECK (delegate_type IS NULL OR delegate_type IN ('member', 'agent'))
    NOT VALID;

COMMENT ON COLUMN issue.delegate_type IS
    'F01: optional partner actor kind (member|agent, never squad). Names who the assignee works with; triggers no run and carries no status.';
COMMENT ON COLUMN issue.delegate_id IS
    'F01: optional partner actor id, paired with delegate_type. Both halves move together; neither is a foreign key.';
