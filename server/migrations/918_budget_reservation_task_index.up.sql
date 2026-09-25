-- Settlement and the per-task reservation lookup address reservations by task.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_budget_reservation_task ON budget_reservation(task_id);
