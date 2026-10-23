-- Photos are checked before they are published. A reviewer held a report
-- whose photo showed a readable serial-number label and a QR code, and a
-- "PCB photo" in another was a photo of a monitor showing someone else's
-- board, of another revision than the sender's unit.
--
-- own_photo: the sender confirmed the photo is of their own camera, taken
-- by them -- the send form asks it. NULL: not asked (ipctool, an agent, a
-- form older than the question).
--
-- A reviewer may publish a photo's copy with regions blurred, or keep the
-- photo private: report_files.public_sha256 names the blurred copy (a new
-- content-addressed file, written once) or is NULL, and the original stays
-- in sha256 for its sender and the maintainers. public_mime is the copy's
-- type when it is not the original's (a WebP blurred into a JPEG).
ALTER TABLE report_files ADD COLUMN own_photo boolean;
ALTER TABLE report_files ADD COLUMN public_mime text
    CHECK (public_mime IN ('image/jpeg', 'image/png'));

-- Every such decision, append-only and guarded like the reports: what the
-- public copy became, the regions blurred, who and when. Not a foreign key
-- to report_files: a file its member removes, or a takedown, takes the row
-- out, and the history stays.
CREATE TABLE report_photo_reviews (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    report_id     text NOT NULL REFERENCES reports ON DELETE RESTRICT,
    position      int NOT NULL,
    -- blur: the public copy has regions blurred; private: no public copy
    -- (not the sender's unit); as_sent: the photo itself, again
    action        text NOT NULL CHECK (action IN ('blur', 'private', 'as_sent')),
    regions       jsonb NOT NULL DEFAULT '[]',
    public_sha256 text CHECK (public_sha256 ~ '^[0-9a-f]{64}$'),
    by            text NOT NULL CHECK (by <> ''),
    at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX report_photo_reviews_by_file ON report_photo_reviews (report_id, position, id DESC);
CREATE TRIGGER report_photo_reviews_guard BEFORE UPDATE OR DELETE ON report_photo_reviews
    FOR EACH ROW EXECUTE FUNCTION reports_guard();
CREATE TRIGGER report_photo_reviews_no_truncate BEFORE TRUNCATE ON report_photo_reviews
    FOR EACH STATEMENT EXECUTE FUNCTION reports_guard();
