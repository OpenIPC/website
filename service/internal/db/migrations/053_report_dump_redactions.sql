-- The redacted copy of a flash dump the maintainers' tools are sent in
-- place of one the backup scan withholds for what a redaction takes out
-- (reports/redact_dump.go): its sum, content-addressed beside the dump, and
-- the kinds taken out. A cache: made again when a rule learnt since finds
-- something in it, and from the dump as sent each time. Nothing a camera's
-- owner sent is changed.
CREATE TABLE report_dump_redactions (
    sha256          text PRIMARY KEY CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    redacted_sha256 text NOT NULL CHECK (redacted_sha256 ~ '^[0-9a-f]{64}$'),
    kinds           text[] NOT NULL DEFAULT '{}',
    made_at         timestamptz NOT NULL DEFAULT now()
);

-- dump_access says when a redacted copy was sent; reasons are the kinds
-- taken out.
ALTER TABLE dump_access DROP CONSTRAINT dump_access_outcome_check;
ALTER TABLE dump_access ADD CONSTRAINT dump_access_outcome_check
    CHECK (outcome IN ('listed', 'served', 'redacted', 'withheld', 'not_found', 'failed'));
