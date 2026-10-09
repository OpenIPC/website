---
title: openipc.org, rebuilt
date: 2026-10-10
summary: The site is now a static bundle in three languages and a small Go service. It also has a board catalogue, the OpenIPC Club, a firmware explorer and this news section.
author: OpenIPC team
---

Over the past six weeks openipc.org has been rebuilt from scratch, and the old
Rails application is gone. This post covers what changed and what you can do
here now.

## A static site, in three languages

Every page is now a file. The pages are built from the repository with Astro
and served by nginx, and they no longer depend on a session or a database.
That makes them fast from anywhere and lets mirrors and caches serve them
safely. The whole site is in English, Russian and Chinese. The home page picks
your language from your browser, and every page keeps it in the address
(`/ru/`, `/zh/`).

The hardware catalogue lives in the repository too. [Supported
hardware](/supported-hardware/featured) lists every SoC we build for, and each
one has an installation wizard that offers only the flash layouts the build
actually fits.

## One Go service behind it

Everything that has to remember something now runs in a single Go service with
PostgreSQL:

- camera uploads to the [Open Wall](/open-wall);
- full flash images, assembled on demand from the builds OpenIPC's CI pushes;
- the [firmware explorer](/firmware-explorer), which shows what each build puts
  on the chip, day by day;
- [kernel crashes](/crashes) that cameras recover from and send in, grouped by
  bug.

## Boards, reports and the OpenIPC Club

The [board catalogue](/cameras/boards) collects camera boards from real
devices: photos, pinouts, factory flash dumps, boot logs, and the Xiongmai
firmware each one runs. If you have a camera, ipctool can send its report
straight from the device, even on stock firmware without curl. The
[report page](/cameras/report) explains how.

The [OpenIPC Club](/club) is where those reports become yours. Sign in with
Telegram, GitHub or an emailed link. You earn stars when a maintainer accepts
what you sent, and for keeping a camera on the Open Wall. Members who want to
be listed appear on the [leaderboard](/club/leaderboard).

## News, here

From now on, announcements are posted on this page. Each post is a Markdown
file in the [website repository](https://github.com/OpenIPC/website), so
publishing one is a pull request like any other change. Posts are written in
English. To follow them in a feed reader, subscribe to
[the Atom feed](/news.atom).
