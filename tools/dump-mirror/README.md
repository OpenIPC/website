# dump-mirror

Fetches the flash dumps owners sent to openipc.org onto a maintainer's machine,
for the maintainer's own tools: agents, firmware analysis, the RE corpus.

1. In `/club`, as a maintainer, open **Tokens for your tools** and make one.
   Name it after the machine, and pin it to that machine's address if it has a
   fixed one. It is shown once.
2. Put it in `~/.config/openipc/token` with mode 0600, or in `OPENIPC_TOKEN`.
3. Run `tools/dump-mirror/dump-mirror sync`, or `--site https://dev.openipc.org`
   to fetch from dev.

The dumps land in `~/openipc-dumps/<chip>/<report>-<position>-<sha12>.bin`. That
directory is 0700, and everything in it is 0600. `index.jsonl` describes each
dump: its report, chip, sensor, flash size, board and when it was received.

The site sends a dump only when its backup scan
(`service/internal/reports/backupscan.go`) read all of it and found nothing of
its owner's: no Wi-Fi password, no cloud key or ID, no private key. A dump with
any of those is **withheld**. So is one the scan cannot read all of: JFFS2
compressed with LZO, LZMA or rubin, or any UBIFS, a
compressed stream outside the firmware's own images that is not gzip or bzip2
(xz, LZMA, zstd, zip), or JFFS2 nodes that are damaged. A withheld dump is answered 409
with the kinds of what is in it, never the values. The mirror remembers it in
`state.json` and asks again after `--retry-days`, because the scan learns to read
more. Every dump asked for is recorded on the site in `dump_access`, which is
append-only.

The token is sent only to an https site, and a redirect is refused rather
than followed. Two syncs never run at once.

## Correcting a report's chip

A dump sent without ipctool's output names no chip, and an unlabelled dump can
turn out to be another chip than the one its U-Boot was built for (r-5uwdebux:
the AV100 SDK's U-Boot, an AK3918AV130 by both its device trees). With a token
made in `/club` with **May also correct a report's chip** ticked:

    dump-mirror chip r-5uwdebux Anyka AK3918AV130 --evidence "device trees: anyka,ak3918av130"

The report stays as sent. The correction is kept with the evidence, the member
and the token. The next `sync` files the dump under its chip in `index.jsonl`.
`dump-mirror chip <id> "" "" --evidence why` takes a correction back.

Keep the directory out of git checkouts and out of external backups. Revoke the
token in `/club` when the machine stops using it.
