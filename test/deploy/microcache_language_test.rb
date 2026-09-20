# frozen_string_literal: true

require 'test_helper'

# The microcache in front of Rails keys on the path and holds a response for
# everyone. That is safe for an asset and unsafe for a page whose language is
# negotiated, and on 2026-09-20 it was demonstrably unsafe in production: a
# request for /open-wall in English returned the Russian page, because a
# Russian visitor had primed the entry within the preceding 60 seconds.
#
#   ru first : <html lang="ru">   X-Cache-Status: MISS
#   en second: <html lang="ru">   X-Cache-Status: HIT
#
# Two decisions had combined. `proxy_ignore_headers ... Vary` told nginx to
# disregard what Rails says the response depends on, and the key was the path
# alone. For /snapshots/<id> the second half was deliberate -- the comment in
# the vhost records it as a flood trade, "one cached copy per id, whichever
# locale rendered first" -- taken when a flood was rotating a ?locale= param
# that #203 has since turned into a redirect.
#
# This file asserts the two properties that keep languages apart. Nothing else
# in the suite reads the vhost.
class MicrocacheLanguageTest < ActiveSupport::TestCase
  VHOST = Rails.root.join('deploy/nginx/sites-available/org.openipc').read.freeze

  # Locations that cache something Rails rendered, split into their bodies.
  def cached_rails_blocks
    VHOST.split(/^    (?=location |# )/)
         .select { |b| b.include?('proxy_cache openipc_micro') && b.include?('proxy_pass http://127.0.0.1:3000') }
  end

  # Language-independent by nature: a stylesheet is the same in every locale.
  LANGUAGE_INDEPENDENT = %r{location ~ \^/\(assets\|fonts\)/}

  test 'no cache is told to disregard what the response varies by' do
    ignoring_vary = VHOST.lines.grep(/proxy_ignore_headers/).select { |l| l.include?('Vary') }

    assert_empty ignoring_vary.map(&:strip), <<~MESSAGE.chomp
      These lines tell nginx to ignore Vary:

      #{ignoring_vary.map { |l| "  #{l.strip}" }.join("\n")}

      Rails declares `Vary: Accept-Language` on every unprefixed page, because
      the language of such a page comes from that header. Ignoring it is what
      served the Russian Open Wall to English visitors for 60 seconds at a
      time. Honour it, or do not cache the page.
    MESSAGE
  end

  test 'a page a session could have changed is never stored for everyone' do
    cached_rails_blocks.each do |block|
      location = block[/location[^{]*/].to_s.strip
      next if block.match?(LANGUAGE_INDEPENDENT)

      assert_includes block, 'proxy_cache_bypass $cookie__openipc_session', <<~MESSAGE.chomp
        #{location} caches a Rails-rendered page but will still serve a cached
        copy to a visitor carrying a session.
      MESSAGE

      assert_match(/proxy_no_cache[^;]*\$cookie__openipc_session/, block, <<~MESSAGE.chomp)
        #{location} caches a Rails-rendered page but will store the copy it
        rendered for a visitor carrying a session.

        session[:locale] overrides Accept-Language until #155 removes that
        write, so such a response is that visitor's language, not the one the
        cache key claims. Storing it hands their language to everyone.
      MESSAGE
    end
  end

  test 'the guard covers the locations that actually render pages' do
    covered = cached_rails_blocks.reject { |b| b.match?(LANGUAGE_INDEPENDENT) }

    assert_operator covered.length, :>=, 2,
                    'expected the Open Wall and per-snapshot caches to be found; the vhost was reshaped'
  end
end
