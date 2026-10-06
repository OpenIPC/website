-- A camera the catalogue does not have yet, as the sender names it on the
-- site's send form (POST /api/v1/club/reports with maker and board instead
-- of model): photos of it and its maker and marking are a report too. A
-- maintainer who publishes it confirms or corrects the proposal, and the
-- decision adds the board (boards.CreateModel) before linking the report to
-- it. The proposal is the sender's word and stays as they wrote it; the
-- board made from it is the reviewer's.
--
-- An owner report's row, guarded like the rest (migration 016): inserted,
-- never changed.
CREATE TABLE report_proposals (
    report_id  text PRIMARY KEY REFERENCES reports ON DELETE RESTRICT,
    maker      text NOT NULL CHECK (length(maker) BETWEEN 1 AND 80),
    -- the PCB marking, or the product's name when there is none to read
    board      text NOT NULL CHECK (length(board) BETWEEN 1 AND 80),
    soc        text NOT NULL DEFAULT '' CHECK (length(soc) <= 40)
);
CREATE TRIGGER report_proposals_guard BEFORE UPDATE OR DELETE ON report_proposals
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_proposals_no_truncate BEFORE TRUNCATE ON report_proposals
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
