# Legacy /images/ files

Four badges and logos from 2022–2023, embedded on pages this project does not
control. Nothing on this site references them, which is why nobody would notice
them disappearing until somebody else's page showed a broken image.

nginx serves them from `/srv/www/shared/images` (see `deploy/nginx/`). They are
kept here because that directory is host-only: it is in no backup, and until
this existed nothing could recreate it. `deploy.sh` installs them on every
deploy, so a rebuilt host gets them back without anybody remembering to.

Not `app/assets/` — the asset pipeline fingerprints filenames, and these have to
keep answering at the exact URLs other people already published.
