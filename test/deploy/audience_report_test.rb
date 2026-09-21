# frozen_string_literal: true

require 'fileutils'
require 'open3'
require 'tempfile'
require 'tmpdir'
require 'test_helper'

# deploy/audience-report.sh --visitors is the only place the project counts
# people rather than requests, and nothing else can check it: the report it
# belongs to needs goaccess, a country database and root, and runs at 03:17
# from cron against a log nobody reads afterwards. A miscount there is silent
# and looks like the audience changing.
#
# The fixture log is fourteen beacon requests and three that are not, composed
# so that every rule in visitors() decides something:
#
#   203.0.113.10   crawler, two views      macOS claiming a 1366px viewport
#   203.0.113.11   crawler, one view       same
#   198.51.100.20  wall only, one view     /open-wall, then a click to github
#   198.51.100.21  wall only, two views    /ru/open-wall then an image
#   198.51.100.30  reader (Firefox)        / twice and /get-started
#   198.51.100.30  reader (iPhone)         / -- same address, other visitor
#   198.51.100.40  reader (Chrome)         /open-wall, /low-latency, one event
#
# and three lines that must not be counted at all: a page view, a stylesheet,
# and /api/a/c.js, which is the beacon script being fetched rather than run.
class AudienceReportTest < ActiveSupport::TestCase
  SCRIPT = Rails.root.join('deploy/audience-report.sh')
  FIXTURE = Rails.root.join('test/fixtures/files/beacon-access.log')

  # Debian's default awk is mawk and this host may have gawk; the program has
  # to give the same answer under both, so the suite runs whichever is `awk`
  # and the mawk case is covered explicitly below where one exists.
  def run_visitors(log = FIXTURE, awk: nil, min: nil)
    env = awk ? { 'PATH' => "#{File.dirname(awk)}:#{ENV.fetch('PATH')}" } : {}
    env['ENGAGED_MIN'] = min.to_s if min
    out, status = Open3.capture2e(env, 'bash', SCRIPT.to_s, '--visitors', log.to_s)
    assert_predicate status, :success?, "audience-report --visitors failed:\n#{out}"
    out
  end

  # The full report needs goaccess, a country database and root, none of which
  # a test host has. Everything this exercises happens after goaccess has run
  # and does not depend on what it produced, so a stub that writes nothing is
  # enough to reach it -- and keeps the test honest about which part is covered.
  # The stub answers `-o csv` from $ENGAGED_CSV and does nothing otherwise, so
  # the HTML report costs nothing and the country split gets a CSV the test
  # controls. What this exercises is the parsing and the bookkeeping around it;
  # geolocation itself is goaccess's and is not under test here.
  GOACCESS_STUB = <<~SH
    #!/bin/sh
    [ -n "${ENGAGED_INPUT_COPY:-}" ] && [ -f "$1" ] && cp "$1" "$ENGAGED_INPUT_COPY"
    for arg in "$@"; do
      if [ "$arg" = csv ]; then
        [ -n "${ENGAGED_CSV:-}" ] && cat "$ENGAGED_CSV"
        exit 0
      fi
    done
    exit 0
  SH

  def stub_goaccess(dir)
    stub = File.join(dir, 'goaccess')
    File.write(stub, GOACCESS_STUB)
    File.chmod(0o755, stub)
    dir
  end

  # goaccess writes CRLF, and its empty columns are bare commas rather than
  # empty quoted fields -- which is what welds a continent row's first two
  # columns together and leaves the country at the end rather than in a field
  # of its own. Both are reproduced here because both broke the parse.
  def geo_csv(counts)
    rows = counts.map.with_index do |(code, visitors), index|
      %("#{index}","0","geolocation","#{visitors * 3}","10.00%","#{visitors}","10.00%",) +
        %("1","0.10%","1","1","1",,,"#{code}"\r\n)
    end
    continent = %("0",,"geolocation","999","99.00%","999","99.00%",) +
                %("1","0.10%","1","1","1",,,"AS Asia"\r\n)
    file = Tempfile.new(['geo', '.csv'])
    file.write(continent + rows.join)
    file.flush
    file
  end

  # The geoip database is pointed at the CSV itself: the script only checks
  # that one is readable, and the stub never opens it.
  def report_env(bin, opts)
    csv = opts[:csv]
    env = { 'PATH' => "#{bin}:#{ENV.fetch('PATH')}" }
    env['ENGAGED_MIN'] = opts[:min].to_s if opts[:min]
    env['ENGAGED_INPUT_COPY'] = opts[:input_copy] if opts[:input_copy]
    return env unless csv

    env['ENGAGED_CSV'] = csv.path
    env['OPENIPC_GEOIP_DB'] = csv.path
    env
  end

  # The log is copied under a dated name because the script takes the day from
  # the filename when it can and from `date` when it cannot, and the history
  # has to be deterministic.
  def run_report(log, outdir, day: '20260921', **opts)
    bin = stub_goaccess(Dir.mktmpdir)
    named = File.join(Dir.mktmpdir, "access-#{day}.log")
    FileUtils.cp(log, named)

    out, status = Open3.capture2e(report_env(bin, opts), 'bash', SCRIPT.to_s, named, outdir)
    assert_predicate status, :success?, "audience-report failed:\n#{out}"
    out
  ensure
    FileUtils.rm_rf(bin) if bin
  end

  # Both series files open with a comment naming their columns and what
  # produced them, so that a file read months later is still attributable.
  def data_rows(path)
    lines = File.readlines(path, chomp: true)
    assert_match(/\A# date\t/, lines.first, "#{File.basename(path)} lost its header")
    lines.drop(1).map { |row| row.split("\t") }
  end

  def count_in(output, label)
    output[/^\s*#{Regexp.escape(label)}\s+(\d+)/, 1]&.to_i
  end

  test 'a visitor is an address and a User-Agent, counted once however often it calls' do
    output = run_visitors

    assert_equal 14, count_in(output, 'beacon requests'),
                 'the three non-beacon lines, /api/a/c.js among them, are not visits'
    assert_equal 7, count_in(output, 'ran the JavaScript'), <<~MESSAGE.chomp
      Seven distinct address+User-Agent pairs made those fourteen requests.

      This is GoatCounter's own definition of a session, which is why the two
      can be compared. Two things in the fixture break a naive count: one
      visitor calls the beacon three times, and one address carries two
      different browsers and is therefore two people.
    MESSAGE
  end

  test 'a device that cannot exist is a crawler, whatever it calls itself' do
    assert_equal 2, count_in(run_visitors, 'impossible device'), <<~MESSAGE.chomp
      The fleet crawling /snapshots announces macOS and reports a 1366px
      viewport, which no Mac has ever had. The test is that contradiction and
      not the browser version it also shares, because the version moves.
    MESSAGE
  end

  # The population reading the gallery is not the population reading the site,
  # and folding the two together overstated the audience threefold the first
  # time it was measured.
  test 'the wall is counted apart from the site' do
    output = run_visitors

    assert_equal 2, count_in(output, 'open wall only')
    assert_equal 3, count_in(output, 'readers'),
                 'a visitor who reaches one page outside the wall is a reader, wall views and all'
    assert_match(/open wall only\s+2\s+1 of them one view and gone/, output,
                 'one of the two viewed a single page; the other viewed two')
  end

  test 'pages are counted once per reader, and only for readers' do
    output = run_visitors

    assert_match(%r{^\s+2\s+/$}, output,
                 'two readers reached the home page; the third view was a reload by one of them')
    assert_match(%r{^\s+1\s+/get-started$}, output, 'the path is url-decoded from the beacon query')
    assert_match(%r{^\s+1\s+/open-wall$}, output,
                 'a reader who also looked at the wall counts on the wall page too')
    assert_no_match(%r{/snapshots/1001}, output,
                    "the crawler's pages are not part of what people read")
  end

  # Review finding on #228. Clicks that leave the site (#183) go through the
  # same endpoint, so an event carries a name where a page view carries a path.
  # Counted as pages they promote a visitor who looked at one image and clicked
  # a link into a reader, and put the event name among what people read.
  test 'an event is not a page, and does not turn a wall visitor into a reader' do
    output = run_visitors

    assert_equal 2, count_in(output, 'open wall only'), <<~MESSAGE.chomp
      198.51.100.20 viewed /open-wall and clicked through to github. The click
      is an event -- e=true, and a name rather than a path -- and must leave
      that visitor where it found them.
    MESSAGE
    assert_match(/open wall only\s+2\s+1 of them one view and gone/, output,
                 'an event is not a page view, so the visitor still viewed exactly one page')
    assert_no_match(/ext:github\.com/, output, 'an event name is not a page anyone read')
    assert_no_match(/business-mail/, output, 'nor is one fired by a reader')
  end

  # Review finding on #228, reproduced at 6,000 paths: `sort | head -10` gives
  # sort a SIGPIPE once its output passes the 64KB pipe buffer, and pipefail
  # turns that into exit 141 for the whole nightly. A day of reader paths is
  # well past 64KB.
  test 'a long page list does not kill the run' do
    Tempfile.create(['many-pages-access', '.log']) do |file|
      6000.times do |n|
        file.write(<<~LINE)
          198.51.100.#{(n % 200) + 1} - - [21/Sep/2026:01:00:00 +0000] "POST /api/a/count?p=%2Fpage-#{n}&t=T&s=1920&b=0&rnd=x#{n} HTTP/2.0" 200 43 "https://openipc.org/page-#{n}" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="en-US,en;q=0.9" peer=198.51.100.#{(n % 200) + 1}
        LINE
      end
      file.flush

      output = run_visitors(file.path)

      assert_equal 200, count_in(output, 'readers')
      assert_equal 10, output.scan(%r{^\s+\d+\s+/page-\d+$}).length,
                   'the ten busiest, and the run still has to succeed'
    end
  end

  # The fingerprint names one fleet, and a fingerprint is always a release
  # behind whoever it describes. This is the check that does not depend on
  # getting it right: `open wall only` is by construction the visitors it did
  # NOT catch, because the classifier takes the impossible ones first -- so a
  # crawl that has changed its viewport lands there whatever else it changed.
  #
  # An earlier version cross-checked the fingerprint against Accept-Language
  # and warned when the two disagreed. That held only while one fleet
  # dominated both signals: the morning the snapshot crawler stopped, it began
  # firing on every run, which is how a warning turns into a line people skip.
  test 'it warns when the gallery is collected rather than browsed' do
    wall = <<~LINE
      203.0.113.20 - - [21/Sep/2026:01:06:00 +0000] "POST /api/a/count?p=%2Fsnapshots%2F1004&t=Open%20Wall&s=1512&b=0&rnd=gggg1 HTTP/1.1" 200 43 "https://openipc.org/snapshots/1004" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36" xff="-" cache=- rt=0.003 urt="0.002" al="en-GB,en;q=0.9" peer=203.0.113.20
    LINE

    Tempfile.create(['collected-access', '.log']) do |file|
      file.write(FIXTURE.read)
      4.times { |i| file.write(wall.gsub('203.0.113.20', "203.0.113.2#{i}").sub('1004', "10#{i + 10}")) }
      file.flush

      output = run_visitors(file.path)

      assert_equal 2, count_in(output, 'impossible device'),
                   'still only the fixture pair: the four added report a viewport a real ' \
                   'machine has, so the fingerprint cannot see them -- which is the point'
      assert_equal 6, count_in(output, 'open wall only')
      assert_equal 3, count_in(output, 'readers')
      assert_match(/WARNING: 6 visitors touched only the gallery against 3 who read the site/, output)
      assert_match(/collected, not browsed/, output)
    end
  end

  test 'the quiet fixture raises no warning' do
    assert_no_match(/WARNING/, run_visitors,
                    'two wall visitors against three readers is a gallery being browsed')
  end

  # Every real browser sends Accept-Language. Counting a visit without one as
  # a reader overstated the audience by a third on 2026-09-21, once the wall
  # crawler had gone and what was left on /ru and /zh became visible: 62 of
  # 154, every one reporting an 800px viewport and no browser token.
  #
  # A line of its own rather than a silent drop, because a few privacy setups
  # do strip the header, and this is the report where someone can judge that
  # and add them back.
  test 'a reader-shaped visit with no Accept-Language is not a reader' do
    quiet = <<~LINE
      203.0.113.30 - - [21/Sep/2026:01:07:00 +0000] "POST /api/a/count?p=%2Fru&t=OpenIPC&s=800&b=0&rnd=hhhh1 HTTP/2.0" 200 43 "https://openipc.org/ru" "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/151.0.0.0 Mobile Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="-" peer=203.0.113.30
    LINE

    Tempfile.create(['quiet-access', '.log']) do |file|
      file.write(FIXTURE.read)
      file.write(quiet)
      file.flush

      output = run_visitors(file.path)

      assert_equal 3, count_in(output, 'readers'), 'the quiet visitor was counted as audience'
      assert_equal 1, count_in(output, 'no Accept-Language')
      assert_no_match(%r{^\s+\d+\s+/ru$}, output,
                      'its page must not appear in what readers read either')
    end
  end

  # Review finding on #244. A day's log spans both formats when a log-format
  # change lands, and rows written before al= existed carry no field at all.
  # Reading those as "this client sent no Accept-Language" takes real readers,
  # and every page they read, out of the audience -- on exactly the day
  # someone goes looking. Absent is not empty.
  test 'a row from before the al field existed is not a silent visitor' do
    old_format = <<~LINE
      198.51.100.50 - - [21/Sep/2026:01:08:00 +0000] "POST /api/a/count?p=%2Fecosystem&t=Ecosystem&s=1920&b=0&rnd=iiii1 HTTP/2.0" 200 43 "https://openipc.org/ecosystem" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002"
    LINE

    Tempfile.create(['mixed-format-access', '.log']) do |file|
      file.write(FIXTURE.read)
      file.write(old_format)
      file.flush

      output = run_visitors(file.path)

      assert_equal 4, count_in(output, 'readers'),
                   'the pre-rollout row was read as a client that omitted the header'
      assert_nil count_in(output, 'no Accept-Language'),
                 'nothing here omitted the header, so the line should not appear at all'
      assert_match(%r{^\s+1\s+/ecosystem$}, output,
                   'and their page went out of the reader list with them')
    end
  end

  # Most of this script is awk held in single-quoted shell strings, and an
  # apostrophe anywhere inside one -- in a comment as readily as in code --
  # closes the string and hands the rest of the program to bash. It happened
  # while writing the history block below, on the word "today's". The nightly
  # runs from cron against a log nobody reads afterwards, so the failure is a
  # line in a mail nobody opens.
  test 'the script parses' do
    out, status = Open3.capture2e('bash', '-n', SCRIPT.to_s)

    assert_predicate status, :success?, "audience-report.sh does not parse:\n#{out}"
  end

  # --- engaged readers (#184) ---
  #
  # `readers` is a deliberately low bar: one page outside the wall. A number
  # that low moves with whatever got linked somewhere yesterday, which makes it
  # a poor thing to compare months with. Engaged is the subset that went past
  # the page they landed on.

  test 'engaged readers are the subset of readers who went deeper' do
    assert_equal 3, count_in(run_visitors, 'readers')

    assert_equal 0, count_in(run_visitors, 'engaged'), <<~MESSAGE.chomp
      Nobody in the fixture reads five pages outside the wall: the busiest
      reader views /, /get-started and / again.
    MESSAGE
    assert_equal 1, count_in(run_visitors(min: 3), 'engaged'),
                 'that same reader has exactly three views outside the wall'
    assert_equal 0, count_in(run_visitors(min: 4), 'engaged')
  end

  test 'the threshold it used is printed, so a number cannot outlive its definition' do
    assert_match(/engaged\s+\d+\s+read 5\+ pages outside the wall/, run_visitors)
    assert_match(/engaged\s+\d+\s+read 2\+ pages outside the wall/, run_visitors(min: 2))
  end

  # The wall split exists because the population looking at the gallery is not
  # the population reading the site. Counting gallery views toward engagement
  # would walk a scrolling image collector straight back into the audience by
  # the other door.
  test 'views of the wall do not make a reader engaged' do
    gallery = (1..6).map do |n|
      <<~LINE
        198.51.100.60 - - [21/Sep/2026:01:09:0#{n} +0000] "POST /api/a/count?p=%2Fsnapshots%2F30#{n}&t=Open%20Wall&s=1920&b=0&rnd=jjjj#{n} HTTP/2.0" 200 43 "https://openipc.org/snapshots/30#{n}" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="en-US,en;q=0.9" peer=198.51.100.60
      LINE
    end.join
    one_page = <<~LINE
      198.51.100.60 - - [21/Sep/2026:01:09:09 +0000] "POST /api/a/count?p=%2Fecosystem&t=Ecosystem&s=1920&b=0&rnd=jjjj9 HTTP/2.0" 200 43 "https://openipc.org/ecosystem" "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36" xff="-" cache=- rt=0.002 urt="0.002" al="en-US,en;q=0.9" peer=198.51.100.60
    LINE

    Tempfile.create(['gallery-access', '.log']) do |file|
      file.write(FIXTURE.read + gallery + one_page)
      file.flush

      output = run_visitors(file.path, min: 2)

      assert_equal 4, count_in(output, 'readers'),
                   'one page outside the wall still makes them a reader'
      assert_equal 1, count_in(output, 'engaged'), <<~MESSAGE.chomp
        Six snapshot views and one page is one page of engagement. Only the
        fixture's three-view reader clears a threshold of two.
      MESSAGE
    end
  end

  # #184 asks for numbers that can be compared with the month before, which
  # means the run has to leave a trail. Three columns wide because engaged
  # cannot be read without the readers it is drawn from.
  test 'the nightly appends one row a day and compares against the run before' do
    Dir.mktmpdir do |outdir|
      first = run_report(FIXTURE, outdir, day: '20260920')

      assert_match(/no previous run to compare with/, first,
                   'there is nothing behind the first row and it should say so')

      second = run_report(FIXTURE, outdir, day: '20260921')

      assert_match(/engaged, previous\s+0\s+on 2026-09-20/, second)

      rows = data_rows(File.join(outdir, 'engaged.tsv'))

      assert_equal [%w[2026-09-20 7 3 0 5], %w[2026-09-21 7 3 0 5]], rows, <<~MESSAGE.chomp
        date, visitors, readers, engaged, threshold -- the threshold because it
        DEFINES the count, and the date normalised because `day` is 20260921
        from a filename and 2026-09-21 from `date`, and two formats in one
        column would order the series by which branch produced it.
      MESSAGE
    end
  end

  # Review finding on #249, and the reason the country split does not filter
  # the log by address. An engaged reader is an address AND a User-Agent; the
  # fixture has one address running an engaged Firefox and a one-page iPhone,
  # so selecting by address alone hands the geolocator a browser that is not
  # in the count and the two populations stop reconciling.
  #
  # Asserted on what goaccess is given rather than on what it returns: the
  # stub cannot geolocate, and a stubbed total would prove only that the stub
  # was believed.
  test 'only the engaged visitors own lines are geolocated, not everyone at their address' do
    Dir.mktmpdir do |outdir|
      given = File.join(outdir, 'given-to-goaccess.log')
      run_report(FIXTURE, outdir, min: 2, input_copy: given,
                                  csv: geo_csv([['CN China', 1]]))
      lines = File.readlines(given)

      assert_equal 1, lines.length, 'one engaged visitor in the fixture at this threshold'
      assert_match(/^198\.51\.100\.30 /, lines.first)
      assert_match(/Firefox/, lines.first, <<~MESSAGE.chomp)
        The iPhone at the same address read one page and is not engaged. Filtering
        by address would have handed its line over too, and goaccess counts a
        visitor per User-Agent, so the country total would have exceeded the
        engaged count it is supposed to break down.
      MESSAGE
    end
  end

  test 'two engaged browsers at one address are both geolocated' do
    extra = (1..2).map do |n|
      <<~LINE
        198.51.100.30 - - [21/Sep/2026:01:1#{n}:00 +0000] "POST /api/a/count?p=%2Fecosystem-#{n}&t=E&s=402&b=0&rnd=kkkk#{n} HTTP/2.0" 200 43 "https://openipc.org/ecosystem-#{n}" "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.7 Mobile/15E148 Safari/604.1" xff="-" cache=- rt=0.002 urt="0.001" al="en-GB,en;q=0.9" peer=198.51.100.30
      LINE
    end.join

    Tempfile.create(['two-browsers', '.log']) do |file|
      file.write(FIXTURE.read + extra)
      file.flush

      Dir.mktmpdir do |outdir|
        given = File.join(outdir, 'given-to-goaccess.log')
        output = run_report(file.path, outdir, min: 2, input_copy: given,
                                               csv: geo_csv([['CN China', 2]]))
        lines = File.readlines(given)

        assert_equal 2, count_in(output, 'engaged'), 'the iPhone now reads three pages as well'
        assert_equal 2, lines.length, 'one line each, so the geolocated population is the counted one'
        assert_equal(1, lines.count { |line| line.include?('Firefox') })
        assert_equal(1, lines.count { |line| line.include?('iPhone') })
      end
    end
  end

  # Where the readers who stayed actually are (#184). The continent row is the
  # trap: goaccess lists every country under its continent, and counting both
  # doubles the total and puts "AS Asia" at the top of the list.
  test 'the country split counts countries and not the continents above them' do
    csv = geo_csv([['CN China', 9], ['RU Russia', 4], ['US United States', 2]])

    Dir.mktmpdir do |outdir|
      output = run_report(FIXTURE, outdir, day: '20260921', csv:, min: 3)

      assert_match(/engaged readers by country/, output)
      assert_match(/9\s+60%\s+CN China/, output, 'nine of fifteen is the share, not of the continent total')
      assert_match(/4\s+27%\s+RU Russia/, output)
      assert_no_match(/AS Asia/, output, <<~MESSAGE.chomp)
        The continent row would be counted on top of the countries inside it.
        Its first two columns arrive welded together because goaccess writes
        an empty column as a bare comma, which is what the parse keys on.
      MESSAGE
    end
  end

  test 'the country split is written per day and compared with the run before' do
    Dir.mktmpdir do |outdir|
      run_report(FIXTURE, outdir, day: '20260920', min: 3,
                                  csv: geo_csv([['CN China', 4], ['PL Poland', 3]]))
      output = run_report(FIXTURE, outdir, day: '20260921', min: 3,
                                           csv: geo_csv([['CN China', 9], ['RU Russia', 2]]))

      assert_match(/engaged readers by country, against 2026-09-20/, output)
      assert_match(/9\s+\d+%\s+CN China\s+\(\+5\)/, output, 'four the day before, nine now')
      assert_match(/2\s+\d+%\s+RU Russia\s+\(\+2\)/, output,
                   'absent from the previous day is zero that day, not an absent column')

      rows = data_rows(File.join(outdir, 'engaged-countries.tsv'))

      assert_equal [%w[2026-09-20], %w[2026-09-20], %w[2026-09-21], %w[2026-09-21]],
                   rows.map { |row| [row.first] }, 'two countries a day, both days kept'
      assert_includes rows, ['2026-09-21', 'CN China', '9']
    end
  end

  # Every number here is a count. An address in this output would make the
  # nightly the individual record /privacy says the site does not keep.
  test 'no address reaches the output' do
    Dir.mktmpdir do |outdir|
      output = run_report(FIXTURE, outdir, day: '20260921', min: 3,
                                           csv: geo_csv([['CN China', 1]]))

      assert_no_match(/198\.51\.100\.\d+/, output)
      assert_no_match(/engaged-address/, output)
    end
  end

  test 'a day run twice replaces its row rather than answering twice' do
    Dir.mktmpdir do |outdir|
      2.times { run_report(FIXTURE, outdir, day: '20260921') }

      rows = data_rows(File.join(outdir, 'engaged.tsv'))

      assert_equal 1, rows.length, 'a re-run after a fix must not leave two answers for one date'
    end
  end

  # --- review findings on #249 ---

  # A day on which nobody was engaged is a result, and the country rows for
  # that date have to become empty. Leaving yesterday's countries standing
  # would show a population that was not there, and the run after would compare
  # against them.
  test 'a day with nobody engaged clears its countries rather than keeping the old ones' do
    Dir.mktmpdir do |outdir|
      run_report(FIXTURE, outdir, day: '20260920', min: 2, csv: geo_csv([['CN China', 1]]))

      assert_includes data_rows(File.join(outdir, 'engaged-countries.tsv')),
                      ['2026-09-20', 'CN China', '1']

      # Nothing reaches five pages in the fixture, so this day is a real zero.
      run_report(FIXTURE, outdir, day: '20260921', min: 5, csv: geo_csv([['CN China', 1]]))
      rows = data_rows(File.join(outdir, 'engaged-countries.tsv'))

      assert_empty rows.select { |row| row.first == '2026-09-21' },
                   'a zero day must record no countries'
      assert_includes rows, ['2026-09-20', 'CN China', '1'], 'and must not disturb another day'
    end
  end

  # The previous run is read from engaged.tsv, which has a row for every run
  # including zero days. Asking the country file instead would skip a zero day
  # and compare against an older, larger one.
  test 'the run before a zero day is the zero day, not the last day with countries' do
    Dir.mktmpdir do |outdir|
      run_report(FIXTURE, outdir, day: '20260919', min: 2, csv: geo_csv([['CN China', 5]]))
      run_report(FIXTURE, outdir, day: '20260920', min: 5, csv: geo_csv([['CN China', 5]]))
      output = run_report(FIXTURE, outdir, day: '20260921', min: 5, csv: geo_csv([['CN China', 5]]))

      assert_match(/engaged, previous\s+0\s+on 2026-09-20/, output,
                   'the zero day is a run and is what the next day follows')
      assert_no_match(/on 2026-09-19/, output)
    end
  end

  # ENGAGED_MIN is overridable, and a delta between two different definitions
  # is not a change in the audience.
  test 'a threshold change suppresses the comparison instead of inventing a trend' do
    Dir.mktmpdir do |outdir|
      run_report(FIXTURE, outdir, day: '20260920', min: 3, csv: geo_csv([['CN China', 1]]))
      output = run_report(FIXTURE, outdir, day: '20260921', min: 2, csv: geo_csv([['CN China', 1]]))

      assert_match(/at a threshold of 3, not 2 -- not comparable/, output)
      assert_no_match(/\(\+\d+, \+\d+%\)/, output, 'no percentage across two definitions')
      assert_match(/engaged readers by country\s+\[/, output,
                   'the country list still prints, just without deltas')
    end
  end

  # A lookup that failed is not a day with no countries. Overwriting the rows
  # with nothing would record a fact nobody established.
  test 'a failed country lookup says so and leaves the rows alone' do
    Dir.mktmpdir do |outdir|
      run_report(FIXTURE, outdir, day: '20260920', min: 2, csv: geo_csv([['CN China', 1]]))

      # Fails only the CSV call. Failing the HTML report as well would kill
      # the run under `set -e` before it reached the country split at all.
      bin = Dir.mktmpdir
      File.write(File.join(bin, 'goaccess'), <<~SH)
        #!/bin/sh
        for arg in "$@"; do
          if [ "$arg" = csv ]; then
            echo boom >&2
            exit 3
          fi
        done
        exit 0
      SH
      File.chmod(0o755, File.join(bin, 'goaccess'))
      named = File.join(Dir.mktmpdir, 'access-20260920.log')
      FileUtils.cp(FIXTURE, named)
      db = Tempfile.new(['db', '.mmdb'])
      db.write('x')
      db.flush

      out, = Open3.capture2e({ 'PATH' => "#{bin}:#{ENV.fetch('PATH')}", 'ENGAGED_MIN' => '2',
                               'OPENIPC_GEOIP_DB' => db.path },
                             'bash', SCRIPT.to_s, named, outdir)

      assert_match(/WARNING: the country split failed -- goaccess exited 3/, out)
      assert_includes data_rows(File.join(outdir, 'engaged-countries.tsv')),
                      ['2026-09-20', 'CN China', '1'], 'the rows from the run that worked survive'
    end
  end

  # The threshold is per day. A log spanning two days would otherwise pool a
  # visitor's two shallow days into one engaged one.
  test 'two shallow days do not add up to one engaged day' do
    second_day = File.read(FIXTURE).gsub('21/Sep/2026', '22/Sep/2026')

    Tempfile.create(['two-days', '.log']) do |file|
      file.write(FIXTURE.read + second_day)
      file.flush

      output = run_visitors(file.path, min: 4)

      assert_equal 0, count_in(output, 'engaged'), <<~MESSAGE.chomp
        The busiest visitor reads three pages on each of two days. Pooled that
        is six and clears a threshold of four; per day it is three and does not.
      MESSAGE
      assert_match(/NOTE: this log spans 2 days/, output)
    end
  end

  test 'mawk and gawk agree' do
    skip 'only one awk on this machine' unless File.executable?('/usr/bin/mawk') &&
                                               File.executable?('/usr/bin/gawk')

    mawk = Dir.mktmpdir
    gawk = Dir.mktmpdir
    File.symlink('/usr/bin/mawk', File.join(mawk, 'awk'))
    File.symlink('/usr/bin/gawk', File.join(gawk, 'awk'))

    assert_equal run_visitors(FIXTURE, awk: File.join(gawk, 'awk')),
                 run_visitors(FIXTURE, awk: File.join(mawk, 'awk')), <<~MESSAGE.chomp
                   The two awks disagree. Debian installs mawk by default and webber-eu
                   has gawk, so the nightly and a run by hand can be the same program
                   reading the same log and printing different numbers.
                 MESSAGE
  ensure
    FileUtils.rm_rf([mawk, gawk].compact)
  end
end
