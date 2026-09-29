-- Tools a camera fetches from openipc.org over plain HTTP: ipctool's builds,
-- pushed by OpenIPC/ipctool's release job (internal/tools/PUSH.md). Stock
-- camera firmware has no curl and no TLS; uget, the downloader an owner
-- pastes in over telnet, speaks HTTP on port 80 and follows no redirects, so
-- GitHub's release links are out of its reach. nginx serves the files at
-- http://openipc.org/<name>; this is what is there.
CREATE TABLE tools (
    name      text PRIMARY KEY CHECK (name IN ('ipctool', 'ipctool-mips32', 'ipctool-arm64')),
    version   text NOT NULL CHECK (version ~ '^[A-Za-z0-9._+-]{1,64}$'),
    sha256    text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    bytes     bigint NOT NULL CHECK (bytes > 0),
    pushed_by text NOT NULL,
    pushed_at timestamptz NOT NULL DEFAULT now()
);
