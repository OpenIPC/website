-- A report's note is the owner's free text, and an owner may paste a MAC or a
-- cloud ID into it. It is redacted like the YAML: the original stays for the
-- maintainers, and only this copy is ever served. Reports stored before this
-- migration have no copy and serve no note.
ALTER TABLE reports ADD COLUMN note_public text NOT NULL DEFAULT '';
