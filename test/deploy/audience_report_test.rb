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
# The fixture log is twelve beacon requests and three that are not, composed
# so that every rule in visitors() decides something:
#
#   203.0.113.10   crawler, two views      macOS claiming a 1366px viewport
#   203.0.113.11   crawler, one view       same
#   198.51.100.20  wall only, one view     /open-wall and gone
#   198.51.100.21  wall only, two views    /ru/open-wall then an image
#   198.51.100.30  reader (Firefox)        / twice and /get-started
#   198.51.100.30  reader (iPhone)         / -- same address, other visitor
#   198.51.100.40  reader (Chrome)         /open-wall and /low-latency
#
# and three lines that must not be counted at all: a page view, a stylesheet,
# and /api/a/c.js, which is the beacon script being fetched rather than run.
class AudienceReportTest < ActiveSupport::TestCase
  SCRIPT = Rails.root.join('deploy/audience-report.sh')
  FIXTURE = Rails.root.join('test/fixtures/files/beacon-access.log')

  # Debian's default awk is mawk and this host may have gawk; the program has
  # to give the same answer under both, so the suite runs whichever is `awk`
  # and the mawk case is covered explicitly below where one exists.
  def run_visitors(log = FIXTURE, awk: nil)
    env = awk ? { 'PATH' => "#{File.dirname(awk)}:#{ENV.fetch('PATH')}" } : {}
    out, status = Open3.capture2e(env, 'bash', SCRIPT.to_s, '--visitors', log.to_s)
    assert_predicate status, :success?, "audience-report --visitors failed:\n#{out}"
    out
  end

  def count_in(output, label)
    output[/^\s*#{Regexp.escape(label)}\s+(\d+)/, 1]&.to_i
  end

  test 'a visitor is an address and a User-Agent, counted once however often it calls' do
    output = run_visitors

    assert_equal 12, count_in(output, 'beacon requests'),
                 'the three non-beacon lines, /api/a/c.js among them, are not visits'
    assert_equal 7, count_in(output, 'ran the JavaScript'), <<~MESSAGE.chomp
      Seven distinct address+User-Agent pairs made those twelve requests.

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

  # Two signals, kept because one of them will rot: the crawler can change its
  # viewport or its User-Agent, and the first sign would be the audience
  # appearing to grow. Every real browser sends Accept-Language.
  test 'it says so when the two bot signals stop agreeing' do
    quiet = <<~LINE
      203.0.113.12 - - [21/Sep/2026:01:06:00 +0000] "POST /api/a/count?p=%2Fsnapshots%2F1004&t=Open%20Wall&s=1512&b=0&rnd=gggg1 HTTP/1.1" 200 43 "https://openipc.org/snapshots/1004" "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36" xff="-" cache=- rt=0.003 urt="0.002" al="-" peer=203.0.113.12
    LINE

    Tempfile.create(['drifted-access', '.log']) do |file|
      file.write(FIXTURE.read)
      3.times { |i| file.write(quiet.sub('203.0.113.12', "203.0.113.1#{i + 2}").sub('1004', "100#{i + 4}")) }
      file.flush

      output = run_visitors(file.path)

      assert_equal 2, count_in(output, 'impossible device'),
                   'the three added crawlers report a viewport a Mac really has'
      assert_match(/WARNING: 5 visitors sent no Accept-Language but 2 look impossible/, output)
      assert_match(/fingerprint in visitors\(\) needs a look/, output)
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
