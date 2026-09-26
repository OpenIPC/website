# frozen_string_literal: true

require_relative 'conformance_helper'

# The upload's answer as bytes on a socket (#291).
#
# Net::HTTP, like every HTTP library worth using, hands back header names
# case-folded, so a server that sends `x-error:` passes every assertion above.
# A camera does not use such a library. Its firmware reads a buffer, and if it
# looks for "X-Error:" or "Retry-After:" at all it looks for those bytes. Rack
# 3 lowercases every header name, and so does HTTP/2 -- which is why these go
# over HTTP/1.1 on a bare socket.
class UploadRawBytesConformanceTest < Conformance::Case
  def assert_header_line(head, name, context)
    assert_match(/\r\n#{Regexp.escape(name)}: /, head,
                 "#{context}: no `#{name}:` line in that casing. The header block was:\n#{head}")
  end

  test 'a refusal says X-Error, capitalised' do
    head = raw_upload(mac: 'not-a-mac')

    assert_match(%r{\AHTTP/1\.1 415 }, head)
    assert_header_line head, 'X-Error', '415'
  end

  test 'a throttled camera is told Retry-After, capitalised' do
    needs_database!
    mac = fresh_mac
    database.seed_frame(mac, 0)

    head = raw_upload(mac: mac)

    assert_match(%r{\AHTTP/1\.1 429 }, head)
    assert_header_line head, 'Retry-After', '429'
  end

  test 'an accepted frame is told Location, capitalised' do
    needs_database!

    head = raw_upload(mac: fresh_mac)

    assert_match(%r{\AHTTP/1\.1 201 }, head)
    assert_header_line head, 'Location', '201'
  end
end
