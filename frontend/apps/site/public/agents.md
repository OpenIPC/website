# openipc.org for AI coding agents

You are helping someone identify an IP camera or recorder board and, if it is
new to OpenIPC, report it to the board catalogue at
<https://openipc.org/cameras/boards>. This page is the whole protocol. Follow
it in order.

## Rules

- Work only on a camera the person you are helping owns or is allowed to open.
  Never on someone else's device, and never over the internet on a camera you
  found there.
- Do not guess or brute-force passwords. Use the console or login the owner
  gives you.
- Ask the owner before you read or send the whole flash. It holds their Wi-Fi
  keys, passwords and cloud IDs. Send it private unless they explicitly want it
  public.
- Nothing you send is public until OpenIPC's maintainers have reviewed it. The
  MAC, the chip's die ID and the cloud ID are replaced with hashes in
  everything published.

## 1. Get ipctool's output

ipctool reads the SoC, sensor, flash layout and firmware off the running board
and prints YAML. Stock firmware has no curl and no TLS, so get it one of these
ways, from a shell on the camera (telnet or UART):

- **uget** (plain HTTP): paste uget in as text. The scripts are at
  `https://openipc.org/uget/uget.arm-<toolchain>-linux.sh`; pick by what
  `ls /lib/ld-*` prints (`ld-uClibc.so.0`: himix100, hisiv500, hisiv510,
  hisiv300; `ld-linux.so.3`: himix200, hisiv600). Then:

      /tmp/uget openipc.org/ipctool > /tmp/ipctool
      chmod +x /tmp/ipctool
      /tmp/ipctool

  `openipc.org/ipctool-mips32` is the Ingenic build, `openipc.org/ipctool-arm64`
  the aarch64 one. uget has no MIPS build; use NFS for Ingenic.

- **NFS**, if the firmware can mount:

      mkdir /tmp/o && mount -o nolock openipc.org:/ipctool /tmp/o
      /tmp/o/ipctool

- Otherwise TFTP, an SD card or UART:
  <https://github.com/OpenIPC/ipctool#alternative-launch-methods>.

Keep the output exactly as printed.

### No shell on the camera

ipctool needs Linux running on the camera. When there is no way in (the
console is muted, the login is unknown, the firmware no longer boots), the
whole flash chip can be the report instead, read another way: from the U-Boot
prompt, through the SoC's boot ROM, or with a programmer. Such an image says
nothing about itself, so the report names its board (`model` in step 4), or
proposes the camera if the catalogue does not have it. Find the board in step
2 by searching rather than identifying.

## 2. Ask whether the board is already known

    POST https://openipc.org/api/v1/boards/identify
    Content-Type: text/plain

    <ipctool's output>

The answer names the SoC as the catalogue does and the boards the output may
be, best first:

```json
{"facts": {"chip_vendor": "HiSilicon", "chip_model": "3516EV300", "sensor": "Sony IMX335"},
 "identify": {"soc": "hi3516ev300", "known": true,
   "matches": [{"model_id": "xiongmai-ipg-50h20pl-s", "model": "IPG-50H20PL-S", "manufacturer": "Xiongmai",
                "url": "/cameras/boards?model=xiongmai-ipg-50h20pl-s", "score": 110,
                "why": ["board code 50H20PL-S", "SoC hi3516ev300"]}]}}
```

`known: true` means a board code in the output is one the catalogue already
has. Show the owner that board's page: it links its OpenIPC firmware and
install instructions. A report is still welcome if the board differs (another
sensor, another flash chip), but it is not needed.

Nothing is stored by this call.

Without ipctool's output, look the board up by its marking (the code on the
silkscreen or the label, or what it is sold as) in the whole catalogue:

    GET https://openipc.org/api/v1/boards

Under `manufacturers[].models[]` are each board's `id`, `model`, `aliases`
and `soc`. <https://openipc.org/cameras/boards> is the same, to browse; a
board's page address ends in `?model=<id>`. That `id` is what step 4's
`model` takes. When the board carries no readable marking,
`/api/v1/boards/search?q=<text>&kind=all` matches lines of the boot logs and
U-Boot consoles others have sent.

## 3. Collect what you can

Useful, in order of value:

1. ipctool's output (required, unless the report names its board; see
   step 4).
2. The boot log: the UART console from power-on to the login prompt.
3. The U-Boot environment: `printenv` at the U-Boot prompt, or
   `ipctool printenv` from Linux.
4. Photos of both sides of the board, sharp enough to read the chip markings.
5. With the owner's agreement, the whole flash: `ipctool backup /tmp/b.bin`
   (it is ipctool's backup format; see step 4), or without ipctool the chip
   as read.

## 4. Send the report

    POST https://openipc.org/api/v1/reports
    Content-Type: multipart/form-data

A report carries ipctool's output (`yaml`, or inside `backup`), or names its
board (`model`, or `maker` and `board` with a photo), or answers another
(`answers`). Parts:

| part | what | limit |
|---|---|---|
| `yaml` | ipctool's output | 256 KB |
| `backup` (file) | `ipctool backup` output: the YAML, a NUL, then each partition as a little-endian uint32 length and its bytes. If sent, `yaml` may be left out. With `model` or a proposed camera, it may instead be a whole flash image as read: the chip's bytes, nothing added, a power of two from 1 MB. | 256 MB |
| `model` | the catalogue's id for the board, when the report has no ipctool output | |
| `answers` | the id of a report waiting for review that this one answers (step 5): photos, a log or a note need no board then | |
| `maker`, `board`, `soc` | a camera the catalogue does not have: its maker, its board's marking or model name, and its SoC if known. Needs at least one `photo`. | 80, 80, 40 chars |
| `consent` | `private` (default: maintainers only) or `public` (published with the report) | |
| `photo` (file, repeatable) | JPEG, PNG or WebP | 20 MB each |
| `boot_log`, `uboot_env`, `note` (file, repeatable) | text | 4 MB each |
| `document` (file) | PDF | 20 MB |
| `channel` | `agent` | |
| `tool` | your name and version, e.g. `claude-code 2.x` | 200 chars |
| `note` | a line of text: where the camera came from, what it is sold as | 4000 chars |

For example (with your own name and version in `tool`):

    curl -F channel=agent -F tool="claude-code 2.x" -F yaml=@ipctool.yml \
         -F boot_log=@boot.txt -F photo=@front.jpg -F photo=@back.jpg \
         https://openipc.org/api/v1/reports

Without ipctool, the chip as read, under its board (here the catalogue's
`xiongmai-nbd8010s-kl-v2`; put the board's own `id` from step 2). curl reads
a value that starts with `<` as a file name, so send none:

    curl -F channel=agent -F tool="claude-code 2.x" -F model=xiongmai-nbd8010s-kl-v2 \
         -F backup=@flash.bin -F boot_log=@boot.txt -F uboot_env=@printenv.txt \
         https://openipc.org/api/v1/reports

`201` answers with the receipt:

```json
{"id": "r-7k2m9q4d", "status": "pending",
 "receipt_url": "https://openipc.org/cameras/report/?id=r-7k2m9q4d",
 "files": [{"kind": "photo", "name": "front.jpg", "bytes": 812345, "sha256": "…"}],
 "identify": {"soc": "hi3516ev300", "known": false, "matches": []}}
```

Give the owner the `receipt_url`. It shows the report's state until a
maintainer files it under its board, and then links there.

`"marking_wanted": true` means nothing in the report says which board it is:
ipctool read no board code, and no `model`, proposal or photo came with it.
The chip alone is on many boards. Ask the owner for a photo of the board's
marking (the code printed on the PCB, or its sticker) and send it as a
report of its own with `answers=<the first one's id>`.

Refusals are JSON `{"error": "..."}` saying what to change: `400` (not
ipctool's output, a malformed backup, a flash image that names no board),
`415` (a file of the wrong type), `413` (too large), `429` (ten reports a day
from one address; retry tomorrow).

## 5. Check on it

    GET https://openipc.org/api/v1/reports/<id>

`status` is `pending`, `asked`, `published`, `rejected` or `withdrawn`. Once
published, `models` names the board it was filed under. `asked` means a
maintainer could not place it as sent and put a question to the sender
instead, in `question`: answer it with a new report sent with
`answers=<this one's id>` (a report sent signed in to the OpenIPC Club is
edited from `/club` instead).

## Machine-readable

- `GET https://openipc.org/api/v1/boards`: the whole catalogue (JSON).
- `GET https://openipc.org/api/v1/boards/search?q=<text>&kind=all`: lines of
  U-Boot consoles, boot logs and notes that match.
- `GET https://openipc.org/api/v1/tools`: the ipctool builds openipc.org
  serves, with their versions.
