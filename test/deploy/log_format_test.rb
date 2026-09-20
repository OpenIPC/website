# frozen_string_literal: true

require 'test_helper'

# deploy/log-report.sh reads this log by POSITION for the first ten fields --
# $1 remote_addr, $7 path, $9 status, $10 body_bytes_sent -- and by `key=`
# label for everything after. #143 set that rule because a day's log spans both
# formats on the day a change lands, so a field inserted in the middle silently
# corrupts every number the report produces for that day and every archived log
# afterwards.
#
# Nothing else in the suite looks at this file.
class LogFormatTest < ActiveSupport::TestCase
  CONF = Rails.root.join('deploy/nginx/conf.d/openipc-logformat.conf').read.freeze

  # The single-quoted fragments nginx concatenates, joined back into the one
  # format string it actually compiles.
  def format_string
    body = CONF[/log_format\s+openipc\s+(.*?);/m, 1]
    body.to_s.scan(/'([^']*)'/).flatten.join
  end

  # Everything up to and including body_bytes_sent, which is what awk indexes.
  POSITIONAL = '$remote_addr - $remote_user [$time_local] "$request" ' \
               '$status $body_bytes_sent'

  test 'the positional prefix is unchanged' do
    assert format_string.start_with?(POSITIONAL),
           <<~MESSAGE.chomp
             The first ten fields of this log are read by position.

             expected it to start: #{POSITIONAL}
             it starts:            #{format_string[0, POSITIONAL.length]}

             deploy/log-report.sh keys on $1, $7, $9 and $10. Inserting or
             reordering anything here changes what those mean, for the whole of
             the day the change lands and for every log archived after it.
             Append instead, with a key= label.
           MESSAGE
  end

  test 'every field after the positional prefix is labelled' do
    tail = format_string.sub(POSITIONAL, '')
    # $http_referer and $http_user_agent were already unparseable by column, so
    # they are the two exceptions the rule was written around.
    tail = tail.sub('"$http_referer" "$http_user_agent"', '')
    unlabelled = tail.split.reject { |token| token.match?(/\A[a-z_]+="?\$/) }

    assert_empty unlabelled, <<~MESSAGE.chomp
      These fields carry no key= label:

      #{unlabelled.map { |t| "  #{t}" }.join("\n")}

      Anything past body_bytes_sent is read with `grep -oP 'key=...'`, never by
      field number, because the quoted referer and user agent contain spaces.
      An unlabelled field is unreadable.
    MESSAGE
  end

  # A header with commas and spaces in it. Unquoted, it would split into
  # several awk fields and shift nothing -- it is last -- but it would be
  # unreadable by the grep that is supposed to find it.
  test 'accept-language is logged, and quoted' do
    assert_match(/al="\$http_accept_language"/, format_string,
                 '#178: the only language signal the origin has without a beacon')
  end

  test 'the fields with list values are quoted' do
    %w[xff urt al].each do |key|
      assert_match(/#{key}="\$/, format_string,
                   "#{key} carries a comma-separated value and must be quoted")
    end
  end

  # The report reads this log with a GoAccess format string that has to describe
  # the same fields in the same order. Nothing connects the two files, and a
  # mismatch is silent: GoAccess parses zero lines and writes an empty report
  # every night until somebody opens one.
  REPORT = Rails.root.join('deploy/audience-report.sh').read.freeze

  def goaccess_format
    REPORT[/--log-format='([^']*)'/, 1].to_s
  end

  test 'the report describes the same fields as the log writes' do
    # GoAccess names what it keeps (%h host, %r request, %s status, %b bytes,
    # %R referer, %u agent, %T time) and ignores the rest with %^. Reduce both
    # sides to the literal text between the fields and they must agree.
    #
    # The timestamp is collapsed whole: nginx writes it as one $time_local
    # where GoAccess has to be told its date and time parts separately, so the
    # two spell the same bracketed field differently on purpose.
    skeleton = lambda do |fmt|
      fmt.gsub(/\[[^\]]*\]/, '[~]').gsub(/%\^|%[a-zA-Z]|\$[a-z_]+/, '~').squeeze('~ ')
    end

    assert_equal skeleton.call(format_string), skeleton.call(goaccess_format),
                 <<~MESSAGE.chomp
                   deploy/audience-report.sh no longer describes this log.

                   nginx writes : #{format_string}
                   goaccess reads: #{goaccess_format}

                   A mismatch is silent -- GoAccess parses nothing and writes an
                   empty report every night.
                 MESSAGE
  end

  test 'the report knows about the accept-language field' do
    assert_includes goaccess_format, 'al=',
                    'the field is written; a reader that stops before it parses nothing'
  end

  # deploy/log-report.sh reads the labelled tail with one anchored regex. The
  # anchor is deliberate -- it is what stops a user agent containing the text
  # `cache=` from being read as the real field -- but anchored to the END of
  # the line it also rejects every line that has a NEW field appended after it.
  #
  # That is what happened: #178 appended al="..." and 2,201 lines had already
  # stopped contributing cache, timing and forwarded-address figures before the
  # review caught it. The rule from #143 is that fields are appended; this is
  # the half of the rule that has to keep reading them.
  REPORT_SH = Rails.root.join('deploy/log-report.sh').read.freeze

  def tail_regexp
    Regexp.new(REPORT_SH[%r{if \(!match\(line, /(.+?)/\)\) return 0}, 1])
  end

  # The shape log-report.sh has to read, with and without what comes after it.
  def log_line(tail)
    '1.2.3.4 - - [20/Sep/2026:00:00:00 +0000] "GET / HTTP/1.1" 200 5 "-" "Mozilla/5.0" ' + tail
  end

  BASE_TAIL = 'xff="-" cache=MISS rt=0.010 urt="0.008"'

  test 'the operations report reads a line in the current format' do
    assert_match tail_regexp, log_line("#{BASE_TAIL} al=\"en-US,en;q=0.9\""),
                 'the al= field is live on production; without this the report reads nothing'
  end

  test 'it still reads a line written before the field was added' do
    assert_match tail_regexp, log_line(BASE_TAIL),
                 'a day of log spans both formats on the day a change lands'
  end

  # Whatever gets appended next.
  test 'it survives a field that does not exist yet' do
    assert_match tail_regexp, log_line(%(#{BASE_TAIL} al="en" host=openipc.org tls=TLSv1.3)),
                 'appending is the documented way to change this log'
  end

  # The reason the regex is anchored at all, from the #143 review.
  test 'a user agent that quotes the tail does not become the tail' do
    spoof = '1.2.3.4 - - [20/Sep/2026:00:00:00 +0000] "GET / HTTP/1.1" 200 5 "-" ' \
            '"Evil xff=\"x\" cache=HIT rt=9.9 urt=\"9.9\"" ' + BASE_TAIL
    matched = spoof[tail_regexp]

    assert matched, 'the real tail should still be found'
    assert_includes matched, 'cache=MISS', 'it read the user agent instead of the real field'
    assert_not_includes matched, 'cache=HIT'
  end
end
