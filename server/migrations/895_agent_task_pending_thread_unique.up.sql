-- Build the new guard before retiring the issue-wide guard.
--
-- Upstream (multica-ai/multica) and this fork relaxed the same pending-slot
-- rule in the same window, in two directions that turn out to be orthogonal:
--
--   upstream  (issue, agent, comment_thread)      one pending task per thread
--   fork F11  (issue, agent) WHERE run_group_id IS NULL   racing attempts
--
-- Taking upstream's index verbatim would re-forbid what F11 exists to allow:
-- its predicate says nothing about run_group_id, so two attempts of one agent
-- inside a group would collide the moment they share a thread. Keeping F11's
-- v3 alongside it would do the mirror-image damage, since v3 ignores the
-- thread and would refuse a second thread's task.
--
-- So the two relaxations compose into a single index rather than coexisting:
-- upstream's thread column, this fork's run-group predicate. Every row either
-- side admitted on its own, this one still admits; nothing new is admitted
-- beyond the intersection the two features already agreed on.
--
-- v3 is retired in the next migration, once this guard is in place.
CREATE UNIQUE INDEX CONCURRENTLY idx_one_pending_task_per_issue_agent_thread
    ON agent_task_queue (issue_id, agent_id, COALESCE(comment_thread_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE run_group_id IS NULL
      AND (status IN ('queued', 'dispatched')
           OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true'));
