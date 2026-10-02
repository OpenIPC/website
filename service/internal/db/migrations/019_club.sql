-- The OpenIPC Club: people who sign in to follow what they sent to the board
-- catalogue, keep their flash dumps private to themselves and the
-- maintainers, and collect stars for what is accepted (internal/club).
--
-- This reverses #288's "no sign-in" deliberately, and only this far: the
-- session cookie is scoped to /api/v1/club, so the pages stay static and
-- cached, and a visitor who never signs in is never sent a cookie.
--
--   member     one person, however many ways they sign in
--   identity   one way in: a Telegram account, a GitHub account, an email
--              address. A GitHub identity in OpenIPC's organisation makes
--              its member a maintainer, who reviews what members send.
--   session    a signed-in browser; only the token's sha256 is stored
--   login      one sign-in in progress: the code in a Telegram link, an
--              emailed link or GitHub's state, tied to the browser that
--              asked for it; ten minutes, used once

CREATE TABLE club_members (
    id          text PRIMARY KEY CHECK (id ~ '^m-[a-z0-9]{10}$'),
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- the member muted the bot (/quiet); the site still lists everything
    quiet       boolean NOT NULL DEFAULT false
);

CREATE TABLE club_identities (
    provider    text NOT NULL CHECK (provider IN ('telegram', 'github', 'email')),
    -- Telegram's and GitHub's numeric user id, or the address in lower case
    subject     text NOT NULL CHECK (length(subject) BETWEEN 1 AND 320),
    member_id   text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    -- what the person is called there: @username, the GitHub login, the address
    handle      text NOT NULL DEFAULT '',
    -- GitHub: a member of the maintainers' organisation at the last sign-in
    maintainer  boolean NOT NULL DEFAULT false,
    -- Telegram: the private chat the bot writes to; NULL after /stop
    chat_id     bigint,
    -- the language the bot writes in: Telegram's, or the page's
    locale      text NOT NULL DEFAULT 'en' CHECK (locale IN ('en', 'ru', 'zh')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    seen_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);
CREATE INDEX club_identities_by_member ON club_identities (member_id);

CREATE TABLE club_sessions (
    token_sha256 text PRIMARY KEY CHECK (token_sha256 ~ '^[0-9a-f]{64}$'),
    member_id    text NOT NULL REFERENCES club_members ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX club_sessions_by_member ON club_sessions (member_id);

CREATE TABLE club_logins (
    code_sha256    text PRIMARY KEY CHECK (code_sha256 ~ '^[0-9a-f]{64}$'),
    provider       text NOT NULL CHECK (provider IN ('telegram', 'github', 'email')),
    -- sha256 of the secret in the asking browser's login cookie; NULL for a
    -- link the bot sends into a member's own chat
    browser_sha256 text CHECK (browser_sha256 ~ '^[0-9a-f]{64}$'),
    email          text,
    -- the member already signed in on the asking browser: what this login
    -- adds is linked to them rather than made a new account
    for_member     text REFERENCES club_members ON DELETE CASCADE,
    -- set when the person finished on the other side (tapped Start)
    member_id      text REFERENCES club_members ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,
    -- set when a browser was signed in by it; a login signs in once
    used_at        timestamptz
);
CREATE INDEX club_logins_by_browser ON club_logins (browser_sha256);
CREATE INDEX club_logins_by_email ON club_logins (lower(email), created_at);
