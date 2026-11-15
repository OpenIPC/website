-- A maintainer's token may also correct what a report says of its camera
-- (reports.Store.SetChip, migration 051): what an agent reading the dumps
-- finds the chip to be from a device tree or a boot log. Opted into when
-- the token is made; every token reads the dumps.
ALTER TABLE club_tokens DROP CONSTRAINT club_tokens_scope_check;
ALTER TABLE club_tokens ADD CONSTRAINT club_tokens_scope_check
    CHECK (scope IN ('dumps:read', 'dumps:read reports:correct'));
