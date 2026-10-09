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
  on `/ru/` or `/zh/` gets the address in their language. Posts are
  in English.
- Run `npm run export -w @openipc/site` in `frontend/` and commit the
  regenerated `src/data/news.json` with the post. A malformed post makes
  the export, `npm test` and CI fail before it can reach the site.
