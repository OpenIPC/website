-- Why the wall refused an upload (internal/variants). The row of a refused
-- upload stays -- the camera was answered 201 -- but its file is deleted, and
-- until now the reason lived only in the log of whichever container refused
-- it. On 2026-10-03 the one camera whose only frame had been refused could
-- not be diagnosed, because a deploy had replaced that container since.
ALTER TABLE snapshots ADD COLUMN refused_reason text;
