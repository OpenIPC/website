-- The backup check (internal/reports/backupscan.go) keeps a flash dump its
-- sender shared publicly private when it finds what is its owner's in it.
-- A reviewer may still publish one as sent, but only over the check: the
-- kinds it found then are the review's overrides, and a later check that
-- finds a kind not among them keeps the dump private again. Empty for
-- every other review.
ALTER TABLE report_photo_reviews ADD COLUMN overrides text[] NOT NULL DEFAULT '{}';
