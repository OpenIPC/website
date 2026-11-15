-- Maintainers' tokens, and the record of every flash dump read with one.
--
-- A private flash dump was sent for OpenIPC's maintainers only. A maintainer's
-- tools -- the agents and the analysis on their own machine -- read the dumps
-- with a token the maintainer minted in /club, instead of a browser session:
-- `Authorization: Bearer <token>` at /api/v1/maint/. Only the token's sha256
-- is stored. A token works only while its member is a maintainer, read again
-- on every request, so leaving the GitHub organisation ends it too.
CREATE TABLE club_tokens (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id    text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    token_sha256 text NOT NULL UNIQUE CHECK (token_sha256 ~ '^[0-9a-f]{64}$'),
    name         text NOT NULL CHECK (name <> '' AND length(name) <= 100),
    scope        text NOT NULL CHECK (scope IN ('dumps:read')),
    -- the one network the token is used from, when its maintainer says so
    allowed_cidr cidr,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    last_used_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '366 days')
);
CREATE INDEX club_tokens_by_member ON club_tokens (member_id, created_at);

-- Every dump asked for with a token: served, or withheld because the backup
-- scan found something of its owner's in it or could not read all of it.
-- Append-only, and unlike the reports with no way to stand the guard down:
-- it is how the promise to the dump's sender is shown to have been kept.
CREATE TABLE dump_access (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at        timestamptz NOT NULL DEFAULT now(),
    token_id  bigint NOT NULL,
    member_id text NOT NULL,
    sha256    text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    ip        inet,
    outcome   text NOT NULL CHECK (outcome IN ('served', 'withheld')),
    -- for withheld: the kinds found and what was not read, never a value
    reasons   text[] NOT NULL DEFAULT '{}'
);
CREATE INDEX dump_access_by_sha ON dump_access (sha256, at);
CREATE FUNCTION dump_access_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'dump_access is append-only (% refused)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END $$;
CREATE TRIGGER dump_access_guard BEFORE UPDATE OR DELETE ON dump_access
    FOR EACH ROW EXECUTE FUNCTION dump_access_guard();
CREATE TRIGGER dump_access_no_truncate BEFORE TRUNCATE ON dump_access
    FOR EACH STATEMENT EXECUTE FUNCTION dump_access_guard();
