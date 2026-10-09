# News

Each file here is one post on openipc.org/news (#212). Publishing an
announcement is a pull request.

- Name the file `<YYYY-MM-DD>-<slug>.md`. The post's address is
  `/news/<slug>`, so pick a slug that will not need changing later.
- Start with a front matter block:

  ```markdown
  ---
  title: What happened, in one line
  date: 2026-10-10        # the same day as the file name
  summary: One or two sentences for the index and the feed.
  author: OpenIPC team    # optional
  ---
  ```

- Write the body in Markdown (GitHub-flavoured: tables, task lists and
  autolinks work). Raw HTML and `javascript:` links are refused.
- Link to the site's pages by path, for example `/cameras/boards`. A reader
  on `/ru/` or `/zh/` gets the address in their language.
- Write the post in English. To translate it, add
  `<YYYY-MM-DD>-<slug>.ru.md` or `.zh.md` beside it, with the same date and
  its own front matter. English is the original: a translation without one is
  refused, and a post with no translation into the reader's language is shown
  to them in English, said so above the article. Each language has its own
  feed, at `/news.atom`, `/ru/news.atom` and `/zh/news.atom`.
- `tools/news/translate.py <lang> <post>` drafts a translation with an LLM and
  checks it: same links, same code spans, same list items, same heading
  levels, no raw HTML. `--check` runs those checks on a translation written by
  hand. It reads its endpoint and key from the environment, so configure it
  before use; the script's docstring says which variables. A draft is a draft
  — read it before committing it.
- Run `npm run export -w @openipc/site` in `frontend/` and commit the
  regenerated `src/data/news.json` with the post. A malformed post makes
  the export, `npm test` and CI fail before it can reach the site.
