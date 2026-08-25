# Rebuilding the WebUI gallery

`/web-interface` shows twelve screenshots of the OpenIPC WebUI. They are
photographs of a real camera, so they go stale: the last set was taken before a
redesign and showed five pages that no longer exist while missing six that do.
Nobody notices, because a screenshot of a page that was deleted still looks like
a screenshot.

Run this when the WebUI has changed shape — in practice once every few months,
or after any release that moves the navigation.

```bash
tools/webui-gallery/run.sh --camera 192.168.1.10
```

It asks for the password, photographs every page named in
`config/webui_gallery.yml`, removes anything that identifies the camera, lays a
scene over the live player, converts each capture to the two sizes the page
ships, and installs them into `app/assets/images/webui/`. Then commit the result
and open a pull request like any other change.

Requirements: Docker, and network access to the camera. Everything else —
Chrome, Playwright, cwebp — is inside the image the script builds.

## What it produces

Two files per screenshot: `<slug>.webp` at 2560px and `<slug>-thumb.webp` at
1200px. The page loads the tile lazily and swaps in the full-size file through
`data-zoom` only when a visitor zooms. Both are 2x for the size they are shown
at, which is the point — the previous gallery was 1x and looked soft on every
Retina display.

The whole set of tiles costs a few hundred kilobytes. If that grows sharply,
look at what changed: a photographic scene compresses far worse than a screen
full of dark chrome.

## Changing which pages appear

`config/webui_gallery.yml` is the gallery. Add an entry and the page shows it,
the tool photographs it, and the test suite starts requiring both its files. The
same file is read by `app/models/webui_gallery.rb`, so the list and the pictures
cannot drift apart.

After a run the tool prints any page the camera offers that the manifest does
not mention. That list is the answer to "what did the redesign add?".

A full run also deletes images the manifest no longer names, so retiring a page
is one deletion in the YAML.

It replaces every tile, too, and one of them can come out badly through no
fault of the camera's software — the log viewer filling with warnings from
something misconfigured on that particular device, say. Keep the previous one:

```bash
git checkout master -- app/assets/images/webui/logs.webp app/assets/images/webui/logs-thumb.webp
```

Nothing depends on the gallery being one moment in time. Say so in the commit
message, so the next person is not puzzled by a tile whose clock disagrees.

## What gets removed, and why it matters

The camera is a real device on a real network, and its address, gateway,
hostname and MAC are printed into the status bar and into most pages.

**The MAC is the one that matters.** `Snapshot` is keyed on MAC and the Open
Wall is public, so publishing a camera's MAC makes that camera's own snapshots
findable by anyone who reads the screenshot.

`redact.js` rewrites the DOM immediately before the shutter: the hostname you
connected to becomes `camera.local`, the address it resolved to becomes
`192.168.1.10`, any MAC becomes `00:11:22:33:44:55`, and every remaining private
address becomes `192.168.1.50`. Add `--map OLD=NEW` for anything specific to
your network that deserves a tidier stand-in — a gateway, say.

The same question is then asked twice. Once inside `shoot.js`, against the very
page that is about to be photographed — a page reloaded a minute later is not
the artifact being installed — and again in `verify.js`, which re-opens every
page from scratch. Both read the page the way a screenshot does: `innerText`
plus the values shown inside inputs and selects, because `fw-network.cgi` is
almost nothing but those and auditing text alone would have exactly the blind
spot the redaction has a second pass to cover.

Both are written as the opposite question — "is anything still here?" — rather
than as a restatement of the rules, so a rule that quietly stopped matching is
still caught. Both run before anything is installed, on purpose.

IPv6 is handled by prefix, not by a general literal match: the camera's own
resolved addresses in any family, plus link-local and unique-local peers. A
regex loose enough to catch every valid IPv6 form also eats clock times and MAC
addresses.

None of this inspects the pixels. Look at the captures before you commit them.

## The scene

Two pages show a live player, and whatever the camera is pointed at is what goes
on the public site. `--scene FILE` lays a still picture over the `<video>` in
the DOM — the same trick as the redaction, applied to the image instead of the
text — fitted to the element's content box so the player's own border and corner
radius still frame it, and cropped with `object-fit: cover` the way real video
fills the element.

Pages that show live video are marked `scene: true` in the manifest. That mark
is what turns a change in the WebUI into a failure rather than a leak: if the
overlay finds no player to cover on a page that claims to have one, the run
stops instead of photographing the camera's own view.

The default scene is `scene/beach-usa.jpg`; see `scene/CREDIT.md` before
changing it, because the page credits it by name.

`--scene none` publishes the camera's own view. It stays because it is
sometimes the right answer — a camera pointed at a test chart, or at something
the project owns and wants to show — and because forcing a substitution on
somebody who has a better picture is not safety, it is a nuisance. The tool
names the pages this affects and tells you to look at those captures before
committing them. Do look.

## Traps

Five things have cost time here. They are all still true.

**Signing in.** The WebUI redirects unauthenticated *browsers* to `/login.html`
rather than answering `401`, so Playwright's `httpCredentials` never sees a
challenge and every page captures as the sign-in form — silently. `curl -u`
works, which makes it look as though authentication is fine. The tool signs in
through the form once and lets the cookie carry the run.

That is not the end of it: the camera can drop the session *in the middle* of a
run, and it does so by serving the sign-in form with a `200`. Everything then
looks like success — twelve pages captured, the redaction check clean, because a
login form has nothing to hide — and a gallery of sign-in forms gets installed
over the real one. It happened here while this tool was being written. So the
tool refuses any page that is not the page it asked for: it signs in again and
retries once, and fails the run if the second attempt lands anywhere else.

**Codecs.** `preview.cgi` decodes H.264/H.265 over MSE. Playwright's bundled
Chromium has no proprietary codecs, so the player falls back to MJPEG and the
badge photographs as a state no ordinary user sees. The image installs branded
Chrome and the tool launches it with `channel: 'chrome'`.

**Pages that act on render.** `fw-reset.cgi` carries a `<pre data-cmd>` that
`run.cgi` executes as the page loads: `sysupgrade -s -n -x --web`, which wipes
the overlay and reboots. `fw-restart.cgi` reboots the same way. Neither can be
photographed at all; the old gallery's `reset.jpg` cannot be retaken. `shoot.js`
refuses to open them even if the manifest names them. Before adding a page to
the manifest, check it with `grep -l data-cmd *.cgi` in the majestic-webui
checkout.

**Pages that answer 404.** A page the WebUI has dropped still answers at the
address it used to live at, and an error document photographs perfectly well.
This tool exists to be run after redesigns that delete pages, so that is not an
exotic case — it is the case. The tool checks the response status, not only
that the browser stayed on the URL it asked for.

**A second subnet.** A rule written for the camera's own `/24` is not enough:
the log viewer surfaces peers from elsewhere on the network. That is why the
sweep collapses *every* private address, not only the camera's, and why the
verifier flags any that reappear.

**Form values.** Addresses live in `value` attributes as well as in text —
`fw-network.cgi` is almost entirely such fields — and a text-node walk does not
see them. `redactInPage` rewrites both.

## Options

```
--camera HOST     address, hostname, or URL. Required.
--user NAME       WebUI user. Default root.
--password PW     prompted for if omitted, which keeps it out of your history.
--scene FILE      picture for the live player, or "none".
--map OLD=NEW     extra literal substitution, repeatable.
--only SLUGS      comma-separated slugs; re-shoot part of the gallery.
--no-install      leave the output in tmp/webui-gallery/out.
--keep            keep the intermediate PNGs, to look at them full size.
```
