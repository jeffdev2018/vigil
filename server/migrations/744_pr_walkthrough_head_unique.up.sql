-- One walkthrough per (pull request, head). This index IS the "at most one run
-- per head" fence: the enqueue inserts the row first and a losing concurrent
-- insert is refused here, so two triggers firing on the same push (webhook and
-- snapshot refresh) can never enqueue two runs for the same commit.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_pr_walkthrough_head
    ON pr_walkthrough (pr_source, pr_id, head_sha);
