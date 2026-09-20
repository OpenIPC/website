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
end
