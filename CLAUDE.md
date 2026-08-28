# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

The OpenIPC project website — a Rails 7.0 app (Ruby 3.1.2, MySQL) that serves the marketing/docs pages, a browsable catalog of supported camera SoCs with per-SoC firmware installation instructions, on-the-fly firmware image assembly, and the "Open Wall" gallery where cameras upload screenshots via an API.

## Commands

- `bin/dev` — start the full dev stack via foreman (`Procfile.dev`): Rails server on **port 3010** (not 3000), `yarn build --watch` (esbuild JS), and `yarn watch:css` (sass→postcss). Use this, not `bin/rails server` alone, or assets won't rebuild.
- `bin/setup` — idempotent dev bootstrap (`bundle`, `db:prepare`, clear logs/tmp, restart).
- `docker compose run --rm web <cmd>` — run anything against Ruby 3.1.7 + MariaDB without
  installing either. `compose.yaml` + `docker/Dockerfile.dev` are the dev/test stack; the
  root `Dockerfile` is the unrelated production build. Use this when the host Ruby does not
  match `.ruby-version` — which is most hosts. Note `bundle exec rubocop`, not bare `rubocop`.
- `bin/rails test` — run tests (Minitest, parallelized across cores, fixtures auto-loaded). The MySQL `test` DB is regenerated from `development`.
- `bin/rails test test/models/admin_test.rb` — single file; append `:LINE` to run one test.
- `bin/rails test:system` — Capybara + selenium system tests.
- `rubocop` — lint (config in `.rubocop.yml`: `rubocop-performance`, line length 120). Baseline on master is 742 offences over 111 files; judge a change by whether it adds any to the files it touches, not by the total.
- `i18n-tasks missing` / `i18n-tasks unused` — audit translations (config in `config/i18n-tasks.yml`); `easy_translate` provides machine translation via `GOOGLE_TRANSLATE_API_KEY`/`DEEPL_TRANSLATE_API_KEY`.
- `tools/webui-gallery/run.sh --camera <host>` — rebuild the WebUI screenshots on `/web-interface` from a real camera. Needs Docker and network access to the camera; everything else is in the image it builds. Run it when the WebUI changes shape (every few months). It redacts the camera's identity, substitutes a scene over the live player, refuses to open the CGIs that reset or reboot on render, and fails the run rather than installing if anything identifying survives. `tools/webui-gallery/README.md` has the traps.
- Asset bundling (normally run by `bin/dev`): `yarn build` (JS → `app/assets/builds/`), `yarn build:css` (sass + autoprefixer). `app/assets/builds/` is gitignored — rebuild after JS/SCSS changes.

## Deploying

The app runs as a container behind the host's nginx; the checkout in
`/srv/www/org-openipc` is no longer what serves traffic. Actions builds every
branch to `ghcr.io/openipc/website:<sha>`, and `openipc-deploy` on the host
installs a tag.

**Validate on dev.openipc.org before production — always.** The procedure, the
verification techniques, and the traps that have cost time here are in
`deploy/DEV-VALIDATION.md`. Read it before your first deploy.

- `openipc-deploy dev <sha>` / `openipc-deploy prod <sha>` — deploy
- `openipc-deploy rollback prod` — back one release, ~13s
- `openipc-deploy status` — tags, rollback target, health
- `deploy/RESTORE.md` — rebuilding from the S3 backup

Two things that bite: `config.assets.compile = false`, so any asset reference
not going through the pipeline 404s in production; and rollback restores the
image but never the schema, so keep migrations additive.

## Architecture

### Domain model
- `Vendor` `has_many :socs` (and `:sensors`). `Soc` (System-on-Chip) carries the firmware metadata: `family`, `model`, `status`, `featured`, `uboot_filename`, `linux_filename`, etc.
- **`Soc.find` and `Vendor.find` are overridden** to look up by `urlname` slug first, then by id, and `to_param` returns `urlname`. URLs use slugs everywhere; both models auto-generate `urlname` from their name/model via a `before_validation`. Keep this in mind — `Model.find(x)` does not behave like stock ActiveRecord here.
- `Camera` (`app/models/camera.rb`) is **not** an ActiveRecord model — it's an `ActiveModel` PORO representing a user's camera configuration (flash type, firmware edition, network, IP/MAC). It holds all the flash-geometry logic (partition offsets/sizes in hex, `lite` vs `ultimate` editions) used to render installation instructions.
- `Firmware` (`app/models/firmware.rb`) is also a plain PORO. `#generate` assembles a complete flash `.bin` by writing uboot + kernel + squashfs rootfs at computed offsets, reading parts from tarballs under `Soc::RELEASES_ROOT` (`/srv/github-releases`) and caching the result in `public/files/`. Driven by `Cameras::SocsController#download_full_image`.
- `Download` is an ActiveRecord model recording one row per firmware image sent (soc, model, flash type, edition, size, bytes). Written from `Cameras::SocsController#download_full_image` via `Download.record`, which logs and returns nil rather than failing a request that has already produced a valid image. Retired after two years by `deploy/purge-snapshots.sh`.
- `Snapshot` is an ActiveRecord model with an ActiveStorage attached `file` (variants: icon/icon2/thumb/fullhd via libvips). It powers the Open Wall. Validations enforce MAC/IP format, a credentials-driven MAC blacklist, and a **15-minute per-MAC rate limit** (`INTERVAL_LIMIT`); the latter two raise `Snapshot::BlacklistedMac` / `Snapshot::TooSoon` which the controller maps to HTTP 403/429.

### Controller areas
- The `/web-interface` gallery is built from `config/webui_gallery.yml` through `WebuiGallery`, and `tools/webui-gallery` photographs a camera from the same manifest, so the page and the pictures cannot drift apart.
- `PagesController` — static, i18n marketing/tool pages. `root` is `pages#introduction`. Most actions just set `@page_title` and render. `config/routes.rb` also contains many redirects to `github.com/openipc/*` repos, including the wiki at `github.com/OpenIPC/wiki` — the old `wiki.openipc.org` host is retired and no route should point at it.
- `Cameras::SocsController` / `Cameras::VendorsController` — the supported-hardware browser (`/supported-hardware/...`, HTML + JSON), the per-SoC installation wizard (`show`/`update` build a `Camera` and render instruction partials), and firmware image download. Note special-case rendering for SigmaStar NAND and HI3536DV100, and the 8MB-flash forces `lite` edition.
- `SnapshotsController` — public Open Wall API + gallery. **CSRF is skipped** (`verify_authenticity_token`) because cameras POST directly. `create` enqueues `PurgeImagesJob` (deletes snapshots >2 days old) and processes images async via `ProcessImagesJob`. `index` uses a raw correlated SQL query to get the latest snapshot per MAC in the last 24h.
- `Admin::*` — Devise-authenticated CRUD (Socs, Vendors, Snapshots) + dashboard. All inherit `AdminController`, which is just `before_action :authenticate_admin!`. Auth model is `Admin` (Devise); there is no public user model.

### Cross-cutting concerns (`app/controllers/concerns/`)
- `Multilang` — locale handling for ~10 languages. Browser-locale detection, the dropdown `locale_switcher` HTML, and `default_url_options`. **`set_locale` exists but its `before_action` is commented out** — locale currently comes from the `?locale=` param / session, not an automatic before_action.
- `RescueHandler` — production-only `rescue_from StandardError` ladder mapping common exceptions to redirects or static `public/{404,500}.html`, and emailing unexpected errors. Disabled in dev/test so errors surface normally.
- `RubyMineHacks` — IDE type-hint shim, included only in development.

### Conventions & gotchas
- Global constants `MAC_ADDRESS_FORMAT` and `IP_ADDRESS_FORMAT` live in `config/initializers/000_settings.rb` (the `000_` prefix forces it to load first, before models reference them).
- Secrets come from Rails encrypted credentials (`config/credentials.yml.enc`), e.g. `Rails.application.credentials.mac.blacklisted` and `.ip.whitelisted`. You need `RAILS_MASTER_KEY` to edit/decrypt.
- i18n locale files are split by namespace: top-level `config/locales/<locale>.yml` plus `pages.<locale>.yml`, `activerecord.<locale>.yml`, `activemodel.<locale>.yml`, `devise.<locale>.yml`. `i18n-tasks` write-rules (in its config) route new keys to the right file.
- Front end is Hotwire (Turbo + Stimulus) + Bootstrap 5, bundled with esbuild/sass (no importmap for app JS despite `bin/importmap` existing). `app/javascript/application.js` holds the (non-Stimulus) page glue.
- **External runtime dependencies that won't exist in a fresh checkout**: the `/srv/github-releases` firmware tarball directory, libvips (image variants), and `melt`/`ffmpeg` (`Snapshot#generate_timelaps` shells out to them). Firmware-assembly and timelapse features can't run without these.
