ALTER TABLE issue DROP COLUMN IF EXISTS contract_revision, DROP COLUMN IF EXISTS contract_risk;
DROP TABLE IF EXISTS watchdog_verdict;
DROP TABLE IF EXISTS issue_watchdog;
