# Sending a kernel crash to openipc.org

The contract between a camera (the WebUI's crash report page, or the
firmware itself) and `POST https://openipc.org/api/v1/crashes`.

## What a camera has

After a kernel oops or panic, pstore keeps the kernel's log across the
reboot. On the next boot `S98crashlog` packs `/sys/fs/pstore/dmesg-*` into
`/etc/crash/crash.tar.gz`, writes `/etc/crash/pending`, and the WebUI shows
"This camera recovered from a crash" with Download and Dismiss. A boot loop
that never reached a record leaves `/etc/crash/failsafe` (`reason=bootlimit`)
instead; include it in the tar as `failsafe` and it is filed as a boot loop.

## The request

`multipart/form-data`, or the bundle alone as the body.

| part | | |
|---|---|---|
| `bundle` | required | the tar.gz as `S98crashlog` made it (a plain tar, or one record as text, also works); at most 256 KB, 1 MB unpacked |
| `mac` | optional | the camera's MAC, as the Open Wall upload sends it. A crash with a MAC is the camera's linked owner's, for stars, whoever sent it; a member sending from `/club` may name only a camera linked to them |
| `firmware` | optional | `/etc/os-release`'s version, e.g. `2.6.10.05-lite` |
| `majestic` | optional | `majestic -v` |
| `soc`, `sensor` | optional | as `ipcinfo` says; the log's own `sensor=… chip=…` line is used when absent |
| `meta` | optional | JSON, at most 64 KB: what helps reproduce it. The firmware writes it as `meta.json` in the bundle (OpenIPC/firmware `S98crashlog`: the build, kernel, command line, chip, sensor, majestic version, and the pipeline's settings **without passwords, keys, accounts or servers**); a bundle carrying it needs no `meta` field, and its `soc` and `sensor` are used when the camera sends neither and the log no longer names them |

```sh
curl -sS -L --post301 --max-time 30 \
  -F bundle=@/etc/crash/crash.tar.gz -F mac="$(cat /sys/class/net/eth0/address)" \
  -F firmware=2.6.10.05-lite -F majestic="$(majestic -v 2>/dev/null | head -1)" \
  https://openipc.org/api/v1/crashes
```

`-L --post301`: a camera in a network the TSPU blocks is sent to openipc.ru
with a 301, and curl repeats a POST across a 301 only when told to.

## The answer

`201` for a new crash, `200` for one this camera already sent (the same
records, however packed), both:

```json
{"id": "c-…", "signature": "6b1f0c2a9e44", "title": "NULL pointer dereference in __wake_up_common ← RGN_PutRegion [open_rgn]",
 "kind": "panic", "duplicate": false, "self_inflicted": false, "url": "https://openipc.org/crashes/#6b1f0c2a9e44"}
```

Show the title and the link; write them to `/etc/crash/sent` so the page can
say it was sent. `422` is a bundle with no crash in it, `413` one too large,
`429` past the limits (20 a day from one address, 5 from one camera), with
`Retry-After`.

## What happens to it

The records are parsed for the fatal trace, the warnings before it, the
kernel's build, the command line, the SoC and sensor, and lines that often
come before a crash (i2c aborts, failed MMZ allocations). Every MAC and IP
address in the log and in `meta` is replaced by a keyed hash; only OpenIPC's
maintainers read that copy. The bundle as sent is kept and never served.
Public: the signature, the chips and sensors it was seen on, and its status.

A crash asked for -- `echo c > /proc/sysrq-trigger` -- is kept but never
listed and never pays: send real ones.

## majestic's own crashes

When majestic itself dies of SIGSEGV, SIGBUS, SIGILL, SIGFPE or SIGABRT it
leaves `/etc/crash/majestic.dump` (the previous one as `majestic.dump.1`):
the registers at the fault, a slice of the faulting thread's stack, its
memory map, and the build-id of every module it had loaded. Send each dump
as its own bundle -- a tar.gz holding `majestic.dump`, and `meta.json` when
the firmware wrote one, or the dump alone as the body -- to the same
address, with the same fields. The answer is the same, with `kind`
`signal`, a title that names only the signal, and a `url` to `/club/crashes`.

These are the maintainers' only. The stack slice is majestic's memory at
the crash and can hold what it was handling (a request, its environment):
it is kept only until the dump is symbolized -- unwound with gdb against
the executable and debuginfo majestic's CI publishes for the build-id, and
the libraries of the firmware build `meta.json` names -- and then deleted,
leaving the backtrace. Never served, never public: no signature of
majestic's appears on `/crashes`. A signal another process sent (a `kill`)
is kept as caused on purpose and pays nothing; majestic aborting itself is
a bug like a fault.

## Sending automatically

Only with the owner's consent: a setting the owner turns on, after which
`S98crashlog` sends each crash it harvests. Without it, nothing leaves the
camera until the owner presses Send.
