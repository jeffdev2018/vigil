-- A goal used as an initiative (F29) needs a start as well as a due date:
-- "when did we commit to this" is what makes an aggregated progress bar
-- readable. Nullable — every existing goal keeps meaning without one.
ALTER TABLE goal ADD COLUMN IF NOT EXISTS start_date DATE;
