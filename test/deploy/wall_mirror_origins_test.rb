# frozen_string_literal: true

require 'test_helper'

# The Open Wall's frames reach a mirror's readers because of six regular
# expressions in production.rb, and nothing else in the suite looks at them.
#
# openipc.ru, openipc.kz and openipc.cloud are reverse proxies in front of this
# origin, and an ordinary reverse proxy does not forward an `Upgrade`: nginx
# speaks HTTP/1.0 upstream unless told otherwise, so the handshake arrives here
# as a plain GET and is answered 404. Two of the three are other people's hosts
# and one of those cannot be reached at all, so the prerendered pages open a
# socket straight at this origin when their own host will not carry one --
# `requestFramesOrFallBack` in frontend/apps/site/src/lib/wall-frames.ts. That
# is cross-origin, and ActionCable admits it only because these expressions say
# so.
#
# Deleting a line here would take the wall down on one mirror and break no
# other test: the failure is a silent 404 on a handshake, invisible in the
# logs, and openipc.org is blocked in Russia at provider level, so for the
# readers of one of these names there is no other way in.
class WallMirrorOriginsTest < ActiveSupport::TestCase
  PRODUCTION = Rails.root.join('config/environments/production.rb').read.freeze

  # The literals as Regexp objects, so this tests what ActionCable will do
  # rather than how the file is spelled.
  ORIGINS = PRODUCTION[/allowed_request_origins\s*=\s*\[(.*?)\]/m, 1]
            .to_s.scan(/%r\{(.*?)\}/m).flatten.map { |source| Regexp.new(source) }.freeze

  # Every name the site answers to, including the two mirrors whose own nginx
  # cannot carry a socket today.
  MIRRORS = %w[
    https://openipc.org https://www.openipc.org https://dev.openipc.org
    https://openipc.ru https://www.openipc.ru
    https://openipc.kz https://openipc.cloud
    https://xn--e1agocfd3c.xn--p1ai
  ].freeze

  test 'the list was found at all' do
    assert_operator ORIGINS.size, :>=, 6,
                    'allowed_request_origins is not where this test looks for it, ' \
                    'so everything below is vacuously true'
  end

  MIRRORS.each do |origin|
    test "a socket from #{origin} is admitted" do
      assert ORIGINS.any? { |pattern| pattern.match?(origin) },
             "#{origin} serves this site; a page there that cannot open a socket " \
             'shows five test cards where the cameras should be'
    end
  end

  # Anchoring is the whole security of the arrangement: these expressions are
  # what stands between "a mirror may open a socket" and "anyone may".
  ['https://openipc.org.example.com', 'https://evil.example', 'http://openipc.ru',
   'https://notopenipc.kz', 'https://openipc.kz.evil.example'].each do |origin|
    test "a socket from #{origin} is refused" do
      refute ORIGINS.any? { |pattern| pattern.match?(origin) },
             "#{origin} is not this site; frames are metered per address and a " \
             'page anywhere could spend a camera\'s budget'
    end
  end
end
