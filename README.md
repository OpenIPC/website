# website

The OpenIPC project's website, [openipc.org](https://openipc.org).

| | |
|---|---|
| `frontend/` | every page: an Astro site (`apps/site`) and its Preact component library (`packages/ui`) |
| `service/` | the Go service and PostgreSQL behind the camera uploads, the Open Wall and the firmware downloads |
| `data/` | the hardware catalogue, the translations and the WebUI gallery manifest, which both of the above read |
| `deploy/` | nginx, the deploy and backup scripts, and how to run them: start with `deploy/DEV-VALIDATION.md` |

Rails was removed in #304; `deploy/GO-CUTOVER.md` records how the site moved
off it.

## Links the project posts

Add `?ref=<tag>` to any openipc.org link the project publishes. The site counts
it once as `ref:<tag>` and then removes it from the address, so the tag shows up
in the analytics dashboard without splitting the page's own figures across a row
per source.

This exists because 39% of human page views arrive with no referrer at all --
Telegram and most apps send none -- so the channels the project controls are
otherwise indistinguishable from someone typing the URL.

| where | tag |
|---|---|
| Telegram pins | `ref=tg-en`, `ref=tg-ru`, `ref=tg-fpv` |
| `OpenIPC/firmware` and `OpenIPC/wiki` READMEs | `ref=readme` |
| YouTube descriptions | `ref=yt` |
| the camera WebUI's link home | `ref=webui` |

Tags are lowercased and stripped to `a-z0-9-`, up to 24 characters. Anything
else is dropped rather than recorded, because the value lands in a dashboard as
a row name and arrives from wherever anyone chose to paste the link.
