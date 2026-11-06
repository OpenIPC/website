-- The sensor a review read from a report, for a report whose YAML does not
-- name one: r-2tjtf8fs's camera hangs in ipctool's sensor probe, and its boot
-- log shows ar0237.ko loaded. On the link, as a unit's sensor is one board's.
--
-- It changes only through reports.Store.SetSensor, which stands the guard
-- down for that one statement: the trigger of migration 016 still refuses any
-- other UPDATE. The report's own sensor column stays as sent.
ALTER TABLE report_models
    ADD COLUMN sensor text NOT NULL DEFAULT '' CHECK (length(sensor) <= 100);
