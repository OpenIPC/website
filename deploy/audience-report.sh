#!/bin/bash
# Yesterday's audience, people only, as a self-contained HTML page.
#
# Every audience figure this project has had was produced by retyping awk over
# the access log: the 2026-08-25 study, the traffic table in #142, the
# 2026-09-20 proposal. Each run cost a working day, each found a trap the last
# one had missed, and none of the numbers can be compared with each other
# because the method drifted between them. This is the method, written down and
# run nightly, so that next month's number means the same thing as this one
# (#180).
#
# GoAccess does the report. What it cannot do is decide who is a person, and on
# this site that is the whole difficulty: about nine requests in ten are
# automated, and `--ignore-crawlers` only knows the ones that say so.
#
#   deploy/audience-report.sh [log] [outdir]
#   deploy/audience-report.sh --visitors [log]   # the visitor count on its own
#
# Installed as /usr/local/sbin/openipc-audience-report by
# deploy/install-metrics.sh and run by cron; see deploy/cron.d/openipc-metrics.
set -euo pipefail

# Overridable so the country split can be exercised without the installed
# database -- and so a run by hand can point at a copy.
db=${OPENIPC_GEOIP_DB:-/var/lib/GeoIP/dbip-country-lite.mmdb}

# Page views outside the wall, in one day, before a reader counts as engaged
# (#184). Five is a judgement, not a discovery: on 2026-09-21 the wall-excluded
# day split into 290 addresses with one view, 145 with two to four, 57 with
# five to nine and 64 with ten or more, and the ambiguous band is the middle
# one. Set it deliberately rather than tuning it to flatter a month, and if it
# moves, say so in the memo -- a threshold that drifts makes the series it
# feeds incomparable, which is the thing #180 exists to stop.
#
# It is applied PER DAY, never pooled across a period: over a month a crawler
# fetching one page a day clears five and is counted as a reader, where per day
# it never does.
ENGAGED_MIN=${ENGAGED_MIN:-5}

# How many visitors, as opposed to how many requests. GoatCounter answers this
# for pages -- every number it stores is already deduplicated, one visitor per
# page per eight-hour session -- but it cannot answer it for the site, because
# the deduplication is per page and a reader of three pages is three rows.
# Summing its columns is not a visitor count, and its own dashboard totals have
# the same property.
#
# The session it deduplicates on is a hash of address and User-Agent, held in
# memory for eight hours and never written down, so the figure cannot be
# recovered from its database afterwards. It can be recomputed here: the beacon
# request is in this log, and the same pair identifies the same visitor.
# Counting a day's distinct pairs is slightly looser than GoatCounter -- someone
# who returns after nine hours is two sessions there and one visitor here.
#
#   deploy/audience-report.sh --visitors [log]
#
# Printed by the nightly run as well. No goaccess, no country database and no
# root for this mode, so it can be run against any log by hand.
visitors() {
  awk -F'"' -v engaged_min="$ENGAGED_MIN" '
    function decode(s,   out, hi, lo) {
      out = ""
      while (match(s, /%[0-9A-Fa-f][0-9A-Fa-f]/)) {
        hi = hex[substr(s, RSTART + 1, 1)]
        lo = hex[substr(s, RSTART + 2, 1)]
        out = out substr(s, 1, RSTART - 1) sprintf("%c", hi * 16 + lo)
        s = substr(s, RSTART + RLENGTH)
      }
      return out s
    }

    BEGIN {
      for (i = 0; i <= 9; i++) hex[i "" ] = i
      split("a b c d e f", letters, " ")
      for (i = 1; i <= 6; i++) { hex[letters[i]] = 9 + i; hex[toupper(letters[i])] = 9 + i }
    }

    # The beacon, and only the beacon: a client that ran the JavaScript. The
    # script it runs is /api/a/c.js, which is a static fetch and says nothing
    # about whether it executed.
    $2 !~ /\/api\/a\/count/ { next }

    {
      split($1, addr, " ")
      visitor = addr[1] "|" $6

      width = 0
      if (match($2, /[?&]s=[0-9]+/)) width = substr($2, RSTART + 3, RLENGTH - 3) + 0
      # Absent is not the same as empty. A log spanning the #143 rollout holds
      # rows written before al= existed at all, and reading those as "this
      # client sent no Accept-Language" would take real readers -- and every
      # page they read -- out of the audience on exactly the day someone looks.
      # Only a field that is present and empty says anything about the client.
      language = ""
      has_language = 0
      if (match($0, /al="[^"]*"/)) {
        has_language = 1
        language = substr($0, RSTART + 4, RLENGTH - 5)
      }
      path = ""
      if (match($2, /[?&]p=[^& ]*/)) path = decode(substr($2, RSTART + 3, RLENGTH - 3))

      calls++
      seen[visitor] = 1

      # A device that does not exist. The fleet crawling /snapshots since the
      # beacon went up announces macOS and reports a 1,366 px viewport, and no
      # Mac has ever had one. This is the test rather than the browser version
      # it also shares, because the version moves and the contradiction does
      # not.
      if ($6 ~ /Macintosh/ && width == 1366) impossible[visitor] = 1

      # Every real browser sends Accept-Language. This used to be a second
      # bot signal, cross-checked against the fingerprint above; that only
      # worked while one fleet dominated both, and once the snapshot crawler
      # left it fired on every run. It now separates reader-shaped visits
      # instead of second-guessing the fingerprint.
      if (has_language && (language == "-" || language == "")) quiet[visitor] = 1

      # The gallery and the images in it, in any locale. Kept apart from the
      # rest of the site because the population reading them is not the
      # population reading the site: in the first three hours measured, 369 of
      # the 518 non-crawler visitors touched nothing else, 349 of those viewed
      # exactly one image and left, and only 103 had asked for the stylesheet.
      # That is the proxy-checker the beacon was installed to identify, and
      # folding it into a people count overstates the audience threefold.
      #
      # Only page views decide any of that. #183 sends clicks that leave the
      # site through the same endpoint, carrying e=true and a name rather than
      # a path -- ext:github.com, business-mail, tg-join. Counted as pages they
      # would promote a visitor who looked at one image and clicked a link into
      # a reader, and put an event name in the list of what people read. Both
      # tests: e=true is the marker GoatCounter sets, and a page path here is
      # always window.location.pathname, so it begins with a slash.
      event = ($2 ~ /[?&]e=true/) || (path != "" && substr(path, 1, 1) != "/")

      if (path != "" && !event) {
        if (path ~ /^\/(ru\/|zh\/)?(open-wall|snapshots)(\/|$)/) wall[visitor]++
        # Depth, counted outside the wall only. views_by below includes wall
        # views because wall-once needs them; this one must not, or a gallery
        # visitor who scrolled five images would read as an engaged reader,
        # which is the population the wall split exists to keep separate.
        #
        # One line kept per visitor, the first: it is what the country split
        # geolocates, and keeping the line belonging to THAT visitor is what
        # makes the two populations the same. See engaged_countries.
        else {
          if (!(visitor in elsewhere)) rep[visitor] = $0
          elsewhere[visitor] = 1
          site_views[visitor]++
        }
        views[path SUBSEP visitor] = 1
        views_by[visitor]++
      }
    }

    END {
      for (v in seen) {
        total++
        if (v in impossible) { crawlers++; continue }
        if (v in elsewhere) {
          # Reader-shaped, but every real browser sends Accept-Language. Its
          # own line rather than a share of the audience: on 2026-09-21, with
          # the wall crawler gone, 62 of 154 visitors sent none, all of them
          # reporting an 800 px viewport and no browser token, all of them
          # asking for /ru or /zh. Counting those as readers overstates the
          # audience by a third.
          #
          # Named rather than dropped, because a few privacy setups do strip
          # the header and this is the report where someone can judge that
          # and add them back. The rule is the one the wall split follows: an
          # ambiguous population gets a line, never a share of "people".
          if (v in quiet) no_language++
          else {
            readers++; person[v] = 1
            # A reader who went past the page they landed on. Readers is a
            # low bar by design -- one page outside the wall -- and a number
            # that low moves with whatever was linked somewhere yesterday.
            # This is the one that answers "did anyone stay", and it is a
            # SUBSET of readers rather than its own classification: every
            # test above has already been applied, so the impossible device
            # and the visitor with no Accept-Language cannot reach it.
            if (site_views[v] >= engaged_min) {
              engaged++
              engaged_line[v] = rep[v]
            }
          }
        }
        else if (v in wall) { wall_only++; if (views_by[v] == 1) wall_once++ }
        # Events but no page view at all. analytics.js counts every turbo:load,
        # so this is a page beacon that was blocked or lost rather than a way
        # of browsing; it is kept out of the counts above rather than quietly
        # making someone a reader, and printed only when it happens.
        else events_only++
      }

      for (k in views) {
        split(k, part, SUBSEP)
        if (part[2] in person) page[part[1]]++
      }

      printf "calls %d\n", calls
      printf "visitors %d\n", total
      printf "crawler %d\n", crawlers
      printf "wall-only %d\n", wall_only
      printf "wall-once %d\n", wall_once
      printf "readers %d\n", readers
      printf "engaged %d\n", engaged
      printf "engaged-min %d\n", engaged_min
      # Internal, for the country split below: one log line per engaged
      # VISITOR, which is the address and User-Agent pair counted above and
      # not merely the address. visitor_report never prints these and neither
      # does anything else -- an address in the output would make this report
      # the individual record the privacy page says the site does not keep.
      for (v in engaged_line) printf "engaged-line %s\n", engaged_line[v]
      printf "events-only %d\n", events_only
      printf "no-language %d\n", no_language
      for (p in page) printf "page %d %s\n", page[p], p
    }
  ' "$1"
}

# The block the nightly prints and `--visitors` prints on its own.
#
# Takes the counts rather than the log, because the nightly needs the same
# numbers twice -- once here and once for the history below -- and a second
# pass over a seventy-megabyte log to re-derive them would be a pass nobody
# reading the output could account for.
visitor_report() {
  local counts=$1

  local calls total crawler wall_only wall_once readers engaged engaged_min events_only quiet
  calls=$(awk '$1 == "calls" { print $2 }' <<< "$counts")
  total=$(awk '$1 == "visitors" { print $2 }' <<< "$counts")
  crawler=$(awk '$1 == "crawler" { print $2 }' <<< "$counts")
  wall_only=$(awk '$1 == "wall-only" { print $2 }' <<< "$counts")
  wall_once=$(awk '$1 == "wall-once" { print $2 }' <<< "$counts")
  readers=$(awk '$1 == "readers" { print $2 }' <<< "$counts")
  engaged=$(awk '$1 == "engaged" { print $2 }' <<< "$counts")
  engaged_min=$(awk '$1 == "engaged-min" { print $2 }' <<< "$counts")
  events_only=$(awk '$1 == "events-only" { print $2 }' <<< "$counts")
  quiet=$(awk '$1 == "no-language" { print $2 }' <<< "$counts")

  printf '  beacon requests     %8d\n' "$calls"
  printf '  ran the JavaScript  %8d visitors\n' "$total"
  printf '    impossible device %8d  macOS at 1366px, the snapshot crawler\n' "$crawler"
  printf '    open wall only    %8d  %d of them one view and gone\n' "$wall_only" "$wall_once"
  printf '    readers           %8d  reached a page outside the wall\n' "$readers"
  printf '      engaged         %8d  read %d+ pages outside the wall\n' "$engaged" "$engaged_min"
  [ "$quiet" -eq 0 ] ||
    printf '    no Accept-Language%8d  reader-shaped, but no browser omits that header\n' "$quiet"
  [ "$events_only" -eq 0 ] ||
    printf '    events, no page   %8d  a click counted where the page view did not\n' "$events_only"

  # The fingerprint above names one fleet, and a fingerprint is always one
  # release behind whoever it describes. This is the check that does not
  # depend on getting it right: `open wall only` is BY CONSTRUCTION the
  # visitors the fingerprint did not catch -- the classifier takes the
  # impossible ones first -- so a crawl it has stopped recognising lands
  # there whatever it has changed about itself.
  #
  # More of them than readers means the gallery is being collected rather
  # than looked at. On 2026-09-20, before the ids changed, that read 661
  # against 395 and would have said so; in the ninety minutes after, 8
  # against 142.
  #
  # The previous version cross-checked the fingerprint against
  # Accept-Language and warned when the two disagreed. That only held while
  # one fleet dominated both signals: the moment the snapshot crawler left it
  # fired on every run, which is how a warning becomes a line people skip.
  if [ "$wall_only" -gt "$readers" ]; then
    printf '  WARNING: %d visitors touched only the gallery against %d who read the site.\n' \
      "$wall_only" "$readers"
    printf '           The wall is being collected, not browsed. If the fingerprint above\n'
    printf '           is not catching it, that is where to look first.\n'
  fi

  if [ "$readers" -gt 0 ]; then
    echo '  pages, counted once per reader'
    # The ten busiest, taken by awk rather than `head` on purpose: head closes
    # the pipe as soon as it has them, sort dies of SIGPIPE once its output
    # passes the 64KB pipe buffer, and `set -o pipefail` turns that into a
    # failed nightly. A day of reader paths is well past 64KB. Verified: the
    # head version exits 141 on 6,000 paths.
    awk '$1 == "page" { print }' <<< "$counts" |
      sort -k2,2nr -k3,3 |
      awk 'NR <= 10 { printf "    %6d  %s\n", $2, $3 }'
  fi
}

# One row a day, so that a month of them can be compared with the month before
# it -- which is the whole ask of #184 and the reason #180 wrote the method
# down instead of retyping awk. Three numbers wide on purpose: engaged alone
# cannot be read without the readers it is a subset of, and neither can be read
# without the visitor count they are filtered from.
#
# Re-running a day replaces its row rather than appending a second one, so a
# re-run after a fix does not leave the series with two answers for one date.
# Dates are ISO, so a lexical sort is chronological.
record_history() {
  local counts=$1 outdir=$2 day=$3
  local history="$outdir/engaged.tsv" scratch
  local total readers engaged

  # `day` arrives as 20260921 when it came from a log's filename and as
  # 2026-09-21 when it came from `date`, and the nightly and a hand run of an
  # archived log take different branches. Two formats in one column break the
  # ordering this file depends on -- '-' sorts before any digit, so every
  # dashed date would sort ahead of every undashed one regardless of when it
  # happened. One format, chosen here rather than upstream, because only this
  # file cares.
  [[ $day =~ ^[0-9]{8}$ ]] && day="${day:0:4}-${day:4:2}-${day:6:2}"

  total=$(awk '$1 == "visitors" { print $2 }' <<< "$counts")
  readers=$(awk '$1 == "readers" { print $2 }' <<< "$counts")
  engaged=$(awk '$1 == "engaged" { print $2 }' <<< "$counts")

  # The header is written, not sorted into place. A comment character sorts
  # ahead of a digit by byte, but the default locale collates punctuation as
  # though it were absent and puts the line in the middle of the series.
  # LC_ALL=C for the same reason: the order of this file is the comparison.
  mkdir -p "$outdir"
  scratch=$(mktemp)
  [ -f "$history" ] && awk -v d="$day" -F'\t' '!/^#/ && $1 != d' "$history" > "$scratch"
  printf '%s\t%s\t%s\t%s\n' "$day" "$total" "$readers" "$engaged" >> "$scratch"
  {
    printf '# date\tvisitors\treaders\tengaged (%s+ pages outside the wall) -- beacon\n' "$ENGAGED_MIN"
    LC_ALL=C sort "$scratch"
  } > "$history"
  rm -f "$scratch"

  # The previous row is the run before this date, not simply the line above:
  # a backfill of an older day must compare against what preceded IT, or the
  # report claims a change that never happened.
  # Explicit string comparison: awk treats a field that looks numeric as a
  # number, and mawk and gawk need not agree on what looks numeric.
  awk -v d="$day" -v engaged="$engaged" -F'\t' '
    /^#/ { next }
    ($1 "") < (d "") { prev_day = $1; prev = $4 }
    END {
      if (prev_day == "") {
        printf "  no previous run to compare with; the series starts here\n"
        exit
      }
      # The previous number and the change, not the count for today again:
      # that is four lines above this one, and a figure printed twice invites
      # the reader to wonder which of them is the answer.
      #
      # No apostrophes in here: this whole program is a single-quoted shell
      # string, and one in a comment ends it.
      delta = engaged - prev
      if (prev > 0) printf "  engaged, previous   %8d  on %s (%+d, %+.0f%%)\n", prev, prev_day, delta, 100 * delta / prev
      else          printf "  engaged, previous   %8d  on %s (%+d)\n", prev, prev_day, delta
    }
  ' "$history"
}

# Where the readers who stayed actually are.
#
# GoAccess is the only thing on this host that can read the country database --
# there is no mmdblookup and no python binding -- so the split is taken from
# its CSV rather than by looking addresses up directly. In that output the
# geolocation panel carries a continent per row with its countries as children,
# so a country is a geolocation row that HAS a parent index; the continent rows
# have an empty one and would otherwise be counted a second time.
#
# The population is the same one the count above reports, and keeping it that
# way is the whole difficulty. An engaged reader is an address AND a
# User-Agent, so filtering the log by the ADDRESSES of engaged readers is a
# different set: it drags in every other browser at those addresses, and the
# fixture has exactly that shape -- one address running an engaged Firefox and
# a one-page iPhone. GoAccess would have counted both.
#
# So nothing is filtered here. visitors() hands over one line per engaged
# visitor, its own, and that is all GoAccess ever sees. The country totals then
# reconcile with the engaged count by construction rather than by argument.
#
# Those lines never leave $work, which the trap removes. Only counts are
# printed.
engaged_countries() {
  local counts=$1 work=$2 outdir=$3 day=$4
  local history="$outdir/engaged-countries.tsv" scratch

  [[ $day =~ ^[0-9]{8}$ ]] && day="${day:0:4}-${day:4:2}-${day:6:2}"

  sed -n 's/^engaged-line //p' <<< "$counts" > "$work/engaged.log"
  [ -s "$work/engaged.log" ] || return 0
  [ -r "$db" ] || { echo '  no country database; the engaged split needs one'; return 0; }

  goaccess "$work/engaged.log" -o csv \
    --log-format='%h - %^ [%d:%t %^] "%r" %s %b "%R" "%u" xff="%^" cache=%^ rt=%T urt="%^" al="%^" peer=%^' \
    --date-format='%d/%b/%Y' --time-format='%H:%M:%S' \
    --no-progress --geoip-database "$db" 2>/dev/null > "$work/engaged.csv" || true

  # Three things about this CSV, each of which cost a run to find.
  #
  # It is CRLF, so an anchored match needs the carriage return gone first or
  # nothing at the end of a line is ever found.
  #
  # Empty columns are written as bare commas, so a split on the quote-comma
  # does not separate them. That is what sorts the rows for free: a continent
  # row is "0",,"geolocation",... and its first two columns arrive welded
  # together, leaving a percentage in $3, while a country row is
  # "0","0","geolocation",... and splits cleanly. Testing $3 therefore keeps
  # the countries and drops the continents, which would otherwise be counted
  # a second time on top of the countries inside them.
  #
  # The same welding puts the tail of a row at ,,,"CN China", so the country
  # is not $NF either -- it is matched off the end of the line, which is where
  # it always is. $6 is the visitor count.
  awk -F'","' '
    { sub(/\r$/, "") }
    $3 != "geolocation" { next }
    match($0, /"[A-Z][A-Z] [^"]*"$/) {
      printf "%s\t%s\n", $6, substr($0, RSTART + 1, RLENGTH - 2)
    }
  ' "$work/engaged.csv" | sort -rn > "$work/engaged-by-country"

  [ -s "$work/engaged-by-country" ] || return 0

  # A header, rewritten each run so it cannot end up duplicated, saying what
  # the columns are and what produced them -- this file outlives the report it
  # was printed beside. It survives the sort because "#" sorts ahead of any
  # digit, and every reader below skips it.
  mkdir -p "$outdir"
  scratch=$(mktemp)
  [ -f "$history" ] && awk -v d="$day" -F'\t' '!/^#/ && $1 != d' "$history" > "$scratch"
  awk -v d="$day" -F'\t' '{ printf "%s\t%s\t%s\n", d, $2, $1 }' "$work/engaged-by-country" >> "$scratch"
  {
    printf '# date\tcountry\tengaged readers -- beacon, %s via goaccess\n' "$(basename "$db")"
    LC_ALL=C sort "$scratch"
  } > "$history"
  rm -f "$scratch"

  # The run before this date, the same rule record_history follows, so that
  # backfilling an older day compares against what preceded IT.
  awk -v d="$day" -F'\t' '
    /^#/ { next }
    ($1 "") < (d "") { if ($1 != last_day) { last_day = $1; delete prev; } prev[$2] = $3 }
    END { for (c in prev) printf "%s\t%s\t%s\n", "PREV", c, prev[c]; printf "%s\t%s\n", "PREVDAY", last_day }
  ' "$history" > "$work/engaged-prev"

  local prev_day
  prev_day=$(awk -F'\t' '$1 == "PREVDAY" { print $2 }' "$work/engaged-prev")
  # Where these figures come from, printed with them. #184 asks that every
  # number in the memo name its source, and a country column is the one most
  # likely to be quoted away from the report that produced it.
  if [ -n "$prev_day" ]; then
    printf '  engaged readers by country, against %s  [beacon; %s via goaccess]\n' \
      "$prev_day" "$(basename "$db")"
  else
    printf '  engaged readers by country  [beacon; %s via goaccess]\n' "$(basename "$db")"
  fi

  # A country missing from the previous day had no engaged readers that day,
  # so absence is zero and every row can carry a change. The whole column is
  # dropped on the first run instead, where every number would read "+itself".
  awk -F'\t' -v have_prev="${prev_day:+1}" \
      -v total="$(awk -F'\t' '{ s += $1 } END { print s + 0 }' "$work/engaged-by-country")" '
    FILENAME ~ /engaged-prev$/ { if ($1 == "PREV") was[$2] = $3; next }
    shown >= 10 { next }
    {
      shown++
      share = total > 0 ? 100 * $1 / total : 0
      if (have_prev) printf "    %6d  %3.0f%%  %-22s (%+d)\n", $1, share, $2, $1 - ($2 in was ? was[$2] : 0)
      else           printf "    %6d  %3.0f%%  %s\n", $1, share, $2
    }
  ' "$work/engaged-prev" "$work/engaged-by-country"
}

if [ "${1:-}" = '--visitors' ]; then
  visitor_log=${2:-/var/log/nginx/org.openipc.access.log.1}
  [ -r "$visitor_log" ] || { echo "audience-report: cannot read ${visitor_log}" >&2; exit 1; }
  printf 'audience-report --visitors %s\n' "$visitor_log"
  visitor_report "$(visitors "$visitor_log")"
  exit 0
fi

# Monthly, from cron. DB-IP publish a new file each month under a predictable
# name; the previous one keeps working, so a failed fetch is a stale country
# column rather than a missing report, and the script says so instead of
# failing the run.
if [ "${1:-}" = '--refresh-country-db' ]; then
  mkdir -p "$(dirname "$db")"
  for month in "$(date -u +%Y-%m)" "$(date -u -d '1 month ago' +%Y-%m)"; do
    url="https://download.db-ip.com/free/dbip-country-lite-${month}.mmdb.gz"
    if curl -sfL --max-time 300 "$url" | gunzip > "${db}.new" 2>/dev/null && [ -s "${db}.new" ]; then
      mv "${db}.new" "$db"
      echo "country database updated from ${month} ($(stat -c%s "$db") bytes)"
      exit 0
    fi
  done
  rm -f "${db}.new"
  echo 'country database not updated; keeping the one already installed' >&2
  exit 0
fi

log=${1:-/var/log/nginx/org.openipc.access.log.1}
outdir=${2:-/srv/www/shared/reports}

# The mirrors, for logs written before #174 put set_real_ip_from in place. After
# that the address in the log is the visitor's own and these never appear; until
# then all three passed the "fetched the stylesheet" test as one enormous
# person each. Harmless to keep: they cannot match a real client address.
MIRRORS='^(194\.58\.109\.202|87\.199\.131\.93|2\.29\.12\.216)$'

# Self-declared crawlers that GoAccess's own list misses, plus the tools nobody
# means to count. GoAccess handles the rest under --ignore-crawlers.
BOT_UA='bot|crawler|spider|slurp|bingpreview|headless|phantomjs|curl/|wget/|python-requests|go-http-client|libwww|scrapy|facebookexternalhit|semrush|ahrefs|mj12|dotbot|petalbot|yandexbot|censys|zgrab|masscan'

# One address making more than this many page views in a day is not a person
# reading the site. Measured: the busiest genuine session in the 2026-09-20
# sample was well under a hundred, and the client that broke the old "fetched
# the CSS" heuristic on 2026-09-19 was far above it.
PAGE_VIEW_CEILING=200

[ -r "$log" ] || { echo "audience-report: cannot read ${log}" >&2; exit 1; }
command -v goaccess >/dev/null || { echo 'audience-report: goaccess not installed' >&2; exit 1; }

day=$(basename "$log" | grep -oE '[0-9]{8}' || true)
[ -n "$day" ] || day=$(date -u -d yesterday +%Y-%m-%d)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# 1. Addresses that fetched the stylesheet. A browser rendering a page asks for
#    it; a crawler reading HTML does not. This is the strongest single signal
#    available from the origin, and on its own it is not enough -- hence 2.
awk '$7 ~ /\/assets\/application-.*\.css/ { print $1 }' "$log" | sort -u > "$work/fetched-css"

# 2. Take the ones that also behaved like a reader. An address is dropped if it
#    announces itself as a robot, is one of the mirrors, or asked for more pages
#    in a day than a person plausibly reads.
grep -Ev "$MIRRORS" "$work/fetched-css" > "$work/candidates" || true

awk -v bots="$BOT_UA" -v ceiling="$PAGE_VIEW_CEILING" '
  NR == FNR { candidate[$1]; next }
  !($1 in candidate) { next }
  {
    ua = tolower($0)
    sub(/.*" "/, "", ua); sub(/" xff=.*/, "", ua)
    if (ua ~ bots) { robot[$1] = 1; next }
    # A page view, not an asset: HTML is what a person reads.
    if ($7 !~ /\.(css|js|png|jpe?g|svg|woff2?|ico|map|webp|gz|bin)$/ && $9 ~ /^(200|304)$/) pages[$1]++
  }
  END { for (ip in candidate) if (!(ip in robot) && pages[ip] <= ceiling) print ip }
' "$work/candidates" "$log" | sort -u > "$work/humans"

# Field one, not anywhere in the line. `grep -Ff` would admit a request from
# 11.2.3.40 because 1.2.3.4 is a substring of it, and any request whose URL,
# referrer or user agent happens to quote an approved address.
awk 'NR == FNR { keep[$1]; next } ($1 in keep)' "$work/humans" "$log" > "$work/human.log" || true

mkdir -p "$outdir"
report="$outdir/${day}.html"

geo=()
[ -r "$db" ] && geo=(--geoip-database "$db")

# --ignore-crawlers still earns its place: the prefilter above is per address,
# and a crawler sharing an address with a person would otherwise bring its
# requests along with it.
# The static-file list is this site's, not GoAccess's default: without .webp
# and .woff2 the WebUI gallery's thumbnails and the icon font sit in the
# top-PAGES panel, above the pages.
#
# no-query-string finishes that job. The icon font is requested as
# `bootstrap-icons.woff2?24e3eb84...`, and the extension match does not see
# past the query string, so it stayed at the top of the pages panel with 889
# hits until the string was dropped. Dropping it also folds `/open-wall?page=2`
# into `/open-wall`, which is what this report wants to count anyway -- it
# answers "what do people read", not "how did they get there".
#
# http-method and http-protocol off, or the top-pages panel splits every URL
# by protocol and lists "/" three times -- 31,735 on HTTP/2 and 17,568 on
# HTTP/1.1 in the first run of this, which is a fact about clients rather than
# about what people read.
goaccess "$work/human.log" -o "$report" \
  --log-format='%h - %^ [%d:%t %^] "%r" %s %b "%R" "%u" xff="%^" cache=%^ rt=%T urt="%^" al="%^" peer=%^' \
  --date-format='%d/%b/%Y' --time-format='%H:%M:%S' \
  --http-method=no --http-protocol=no \
  --static-file=.webp --static-file=.woff2 --static-file=.woff \
  --static-file=.svg --static-file=.map --static-file=.bin \
  --no-query-string \
  --ignore-crawlers --html-report-title="openipc.org — people, ${day}" \
  "${geo[@]}" >/dev/null 2>&1

# The counts this filtered out, printed because a prefilter nobody can see is a
# prefilter nobody can check.
printf 'audience-report %s\n' "$day"
printf '  all requests        %8d\n' "$(wc -l < "$log")"
printf '  fetched stylesheet  %8d addresses\n' "$(wc -l < "$work/fetched-css")"
printf '  judged people       %8d addresses\n' "$(wc -l < "$work/humans")"
printf '  their requests      %8d\n' "$(wc -l < "$work/human.log")"
printf '  report              %s\n' "$report"

# The visitor count, from the beacon rather than from the stylesheet. Two
# methods that disagree are worth more than one that cannot be checked: the
# addresses above are judged by what they fetched, these by what they ran, and
# the crawler that runs JavaScript passes the first test and fails the second.
beacon_counts=$(visitors "$log")
visitor_report "$beacon_counts"
record_history "$beacon_counts" "$outdir" "$day"
engaged_countries "$beacon_counts" "$work" "$outdir" "$day"
