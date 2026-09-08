-- Follow-up to 826: the seeded native runtime rows landed with the column
-- defaults — visibility 'private', no owner — and canUseRuntimeForAgent
-- refuses a private ownerless runtime for everyone, so no agent could be
-- created on one (the runtime itself claimed fine; the agent gate is what
-- broke). The native runtime is platform infrastructure: public, owned by
-- the workspace's owning member so the UI has someone to attribute it to.
UPDATE agent_runtime r
SET visibility = 'public',
    owner_id = (
        SELECT m.user_id FROM member m
        WHERE m.workspace_id = r.workspace_id AND m.role = 'owner'
        ORDER BY m.created_at, m.user_id
        LIMIT 1
    )
WHERE r.runtime_mode = 'native' AND r.daemon_id = 'native';
