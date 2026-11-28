-- A report sent signed out before claim codes (#620) has none, and its
-- sender cannot claim it from /club. A maintainer who knows whose it is --
-- the sender asked, from the Club account that sent their other reports
-- from the same connection -- gives it to them with `openipc reports claim`
-- (reports.Store.Assign), as a claim would: report_submissions, a claim
-- code made unusable, stars for one published already. Who gave it and why
-- is kept here, once per report, never changed.
CREATE TABLE report_assignments (
    report_id  text PRIMARY KEY REFERENCES reports ON DELETE RESTRICT,
    member_id  text NOT NULL,
    by_who     text NOT NULL CHECK (by_who <> ''),
    evidence   text NOT NULL CHECK (evidence <> '' AND length(evidence) <= 1000),
    at         timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER report_assignments_guard BEFORE UPDATE OR DELETE ON report_assignments
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_assignments_no_truncate BEFORE TRUNCATE ON report_assignments
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
