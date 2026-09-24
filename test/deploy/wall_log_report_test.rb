# frozen_string_literal: true

require 'open3'
require 'test_helper'

# The Open Wall section of deploy/log-report.sh, whose headline figure is an
# invariant rather than a statistic: since #267 no address returns a camera
# frame, so "camera frames served over HTTP" is supposed to read zero and any
# other number means a door reopened.
#
# A number that is meant to be zero is only useful if it can also be non-zero
# for the right reasons and stay zero for the wrong ones, which is what this
# file pins. The detector is the delicate half. `/open-wall/camera/<id>` is the
# HTML page, still served on purpose, painting its frame onto a canvas; only
# the `.jpg` twin ever handed over bytes. A check written as a substring match
# on `open-wall/camera` reports that page as an escaped frame -- which an
# ad-hoc version of this measurement did twice on 2026-09-23, each time costing
# a round of probing to establish that nothing had leaked. The fixture contains
# both spellings, in two locales, so the distinction cannot quietly collapse.
class WallLogReportTest < ActiveSupport::TestCase
  SCRIPT = Rails.root.join('deploy/log-report.sh')
  FIXTURE = Rails.root.join('test/fixtures/files/wall-access.log')
  EARLIER = Rails.root.join('test/fixtures/files/wall-access-earlier.log')

  def run_report(*logs)
    logs = [FIXTURE] if logs.empty?
    out, status = Open3.capture2e('bash', SCRIPT.to_s, *logs.map(&:to_s))
    assert_predicate status, :success?, "log-report.sh failed:\n#{out}"
    out
  end

  def report
    @report ||= run_report
  end

  def wall_section(text = report)
    text[/^Open Wall$.*?(?=\n\nself-declared|\z)/m].to_s
  end

  # The whole awk program is one single-quoted shell argument, so a single
  # apostrophe anywhere inside it -- in prose, in a comment, in "the header's
  # own example" -- closes the quote and the script dies with a syntax error at
  # run time. It cost two round-trips in one sitting to learn that twice, and
  # the symptom is only ever "exit 2", which names neither the character nor
  # the line. Every other test here would catch it, but none of them would say
  # what happened.
  test 'the awk program contains no apostrophe to close its own quoting' do
    lines = File.readlines(SCRIPT)
    opens = lines.index { |l| l.start_with?('cat ') }
    assert opens, 'could not find the line that opens the awk program'

    # Ends at the line that closes the quote, NOT at the end of the file. The
    # first version assumed the awk program was the last thing in the script;
    # when a plain shell section was appended after it, every apostrophe in
    # that perfectly legal shell was reported as a quoting bug.
    offset = lines[(opens + 1)..].index { |l| l.rstrip == "'" }
    assert offset, 'could not find the line that closes the awk program'

    # Report the line number in the FILE, not an offset into the awk body, so
    # the message can be acted on without counting.
    body = lines[(opens + 1), offset].to_a
    offenders = body.each_with_index
                    .select { |line, _| line.include?("'") }
                    .map { |line, i| "line #{opens + 2 + i}: #{line.strip}" }

    assert_empty offenders,
                 'An apostrophe inside the single-quoted awk program ends the ' \
                 'quote. Reword it -- "the usage note at the top", not ' \
                 '"the header\'s own example".'
  end

  test 'it counts the frames that really did leave over HTTP' do
    # The fixture has exactly two: a /wall/ static frame and an original upload
    # through /snapshots/<id>/download, both 200.
    assert_match(/CAMERA FRAMES SERVED OVER HTTP\s+2\b/, wall_section)
  end

  test 'it says plainly that a non-zero reading is a reopened door' do
    assert_match(/meant to be zero/, wall_section,
                 'The count alone does not tell a reader that zero is the ' \
                 'only acceptable value. Without that sentence the number ' \
                 'looks like traffic rather than an alarm.')
  end

  test 'it dates the most recent leak, so history is not read as a regression' do
    # The count cannot distinguish "this happened before the cutover" from
    # "this is happening now", and on the day itself the honest answer was
    # thousands of frames, all of them before 10:17:11. Without a timestamp the
    # headline reads as a live breach every time someone runs it over a log
    # that spans the change.
    assert_match(%r{most recent one\s+23/Sep/2026:10:16:30}, wall_section)
  end

  test 'the HTML camera page is not mistaken for a frame' do
    # Two requests in the fixture, one per locale prefix, both 200 and both
    # ordinary pages. If either is counted the figure above becomes 3 or 4.
    refute_match(/CAMERA FRAMES SERVED OVER HTTP\s+[34]\b/, wall_section,
                 '/open-wall/camera/<id> without an extension is the HTML ' \
                 'page. Counting it invents a frame leak that did not happen.')
  end

  test 'the refusals that enforce the invariant are counted' do
    # Four 410s in the fixture: a /wall/ frame, a download, the .jpg twin of
    # the camera page, and that twin again with a query string -- out of six
    # requests to retired image addresses.
    assert_match(/retired image paths refused\s+4 of 6 requests answered 410/, wall_section)
  end

  test 'a query string does not hide a request for image bytes' do
    # nginx logs the request target, so `.jpg?v=2` is what lands in the log.
    # An expression anchored with `$` right after the extension drops it, and
    # it drops it from the invariant as readily as from the refusal count --
    # so the one metric meant to notice a reopened door would undercount
    # exactly the requests an attacker can produce for free.
    refute_match(/retired image paths refused\s+3 of 5/, wall_section,
                 'The query-string request fell out of the detector.')
  end

  test 'the locale-prefixed .jpg twin still counts as a retired address' do
    # /ru/open-wall/camera/<id>.jpg is in the fixture. If the locale prefix
    # were not stripped it would fall out of the detector entirely and the
    # refusal count above would read 2 of 4.
    refute_match(/retired image paths refused\s+2 of 4/, wall_section)
  end

  test 'it reports the channel that replaced those addresses, by status' do
    assert_match(/frame channel\s+3 requests:/, wall_section)
    assert_match(/101=1/, wall_section)
    assert_match(/404=1/, wall_section)
    assert_match(/429=1/, wall_section)
  end

  test 'it explains what a 404 and a 429 on the channel actually mean' do
    # Both are mine to cause and neither is self-evident from the number: a 404
    # is a proxy that dropped the Upgrade header, a 429 is limit_conn counting
    # open tabs. Whoever reads this report next should not have to rediscover
    # that, as this session did.
    assert_match(/Upgrade header/, wall_section)
    assert_match(/counts open tabs/, wall_section)
  end

  test 'it separates cameras from snapshots, which are different identifiers' do
    # `/snapshots/<20 hex>` is Snapshot::PUBLIC_ID_FORMAT, one per uploaded
    # frame. `/open-wall/camera/<16 hex>` is Snapshot#camera_token, an HMAC of
    # the MAC and therefore one per camera, stable across every frame it sends.
    #
    # The fixture has three distinct snapshot ids confirmed by a 200 (two pages,
    # one behind a locale prefix, plus the original-upload download) and two
    # distinct camera tokens. Reporting three and calling them cameras -- which
    # this did at first -- overstates how many premises were enumerated and
    # ignores the camera permalink altogether.
    assert_match(/distinct cameras exposed\s+2\b/, wall_section)
    assert_match(/distinct snapshots exposed\s+3\b/, wall_section)
  end

  test 'the most recent leak is the latest by time, not the last line read' do
    # The script takes several logs in whatever order the caller names them,
    # and its own header shows a rotated one being passed. Newest-first is the
    # dangerous order: a leak happening now would be reported with yesterday's
    # timestamp, read as pre-cutover history, and dismissed.
    newest_first = wall_section(run_report(FIXTURE, EARLIER))

    assert_match(%r{most recent one\s+23/Sep/2026:10:16:30}, newest_first)
    refute_match(%r{most recent one\s+22/Sep/2026}, newest_first,
                 'The older log won because it was read last.')

    # And the same answer whichever way round they are given.
    assert_match(%r{most recent one\s+23/Sep/2026:10:16:30},
                 wall_section(run_report(EARLIER, FIXTURE)))
  end
end
