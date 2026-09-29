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

## 3. Collect what you can

Useful, in order of value:

1. ipctool's output (required).
2. The boot log: the UART console from power-on to the login prompt.
3. The U-Boot environment: `printenv` at the U-Boot prompt, or
   `ipctool printenv` from Linux.
4. Photos of both sides of the board, sharp enough to read the chip markings.
5. With the owner's agreement, the whole flash: `ipctool backup /tmp/b.bin`
   (it is ipctool's backup format; see step 4).

## 4. Send the report

    POST https://openipc.org/api/v1/reports
    Content-Type: multipart/form-data

Parts, all optional except the first:

| part | what | limit |
|---|---|---|
| `yaml` | ipctool's output | 256 KB |
| `backup` (file) | `ipctool backup` output: the YAML, a NUL, then each partition as a little-endian uint32 length and its bytes. If sent, `yaml` may be left out. | 256 MB |
| `consent` | `private` (default: maintainers only) or `public` (published with the report) | |
| `photo` (file, repeatable) | JPEG, PNG or WebP | 20 MB each |
| `boot_log`, `uboot_env`, `note` (file, repeatable) | text | 4 MB each |
| `document` (file) | PDF | 20 MB |
| `channel` | `agent` | |
| `tool` | your name and version, e.g. `claude-code 2.x` | 200 chars |
| `note` | a line of text: where the camera came from, what it is sold as | 4000 chars |

For example:

    curl -F channel=agent -F tool="<you>" -F yaml=@ipctool.yml \
         -F boot_log=@boot.txt -F photo=@front.jpg -F photo=@back.jpg \
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

Refusals are JSON `{"error": "..."}` saying what to change: `400` (not
ipctool's output, a malformed backup), `415` (a file of the wrong type),
`413` (too large), `429` (ten reports a day from one address; retry tomorrow).

## 5. Check on it

    GET https://openipc.org/api/v1/reports/<id>

`status` is `pending`, `published`, `rejected` or `withdrawn`. Once published,
`models` names the board it was filed under.

## Machine-readable

- `GET https://openipc.org/api/v1/boards`: the whole catalogue (JSON).
- `GET https://openipc.org/api/v1/boards/search?q=<text>&kind=all`: lines of
  U-Boot consoles, boot logs and notes that match.
- `GET https://openipc.org/api/v1/tools`: the ipctool builds openipc.org
  serves, with their versions.
