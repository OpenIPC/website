-- A vendor's stock firmware as it is distributed (#600): an update archive
-- or the images it unpacks to, kept for the maintainers and never served.
ALTER TABLE report_files DROP CONSTRAINT report_files_kind_check;
ALTER TABLE report_files ADD CONSTRAINT report_files_kind_check
    CHECK (kind IN ('backup', 'photo', 'boot_log', 'uboot_env', 'note', 'document', 'firmware'));
