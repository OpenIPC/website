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

  # #204 kept session-carrying requests out of these caches by hand, because
  # session[:locale] could change what was rendered. #155 removed that write,
  # and with it the reason -- so the guard is now the honest one: nginx is told
  # nothing to ignore, which means a response that sets a cookie is not stored
  # at all.
  #
  # That is a better guard than the explicit bypass it replaces. The bypass
  # protected against one known cause; this protects against any future code
  # that starts writing the session, and it fails in the safe direction -- the
  # page stops being cached rather than being cached with somebody's cookie in
  # it.
  test 'a response that sets a cookie is never stored for everyone' do
    cached_rails_blocks.each do |block|
      next if block.match?(LANGUAGE_INDEPENDENT)

      location = block[/location[^{]*/].to_s.strip
      ignored = block.lines.grep(/proxy_ignore_headers/).join

      assert_not_includes ignored, 'Set-Cookie', <<~MESSAGE.chomp
        #{location} is told to ignore Set-Cookie, so it will store a response
        that carries one and hand that visitor's session to everyone else.

        Nothing public sets a cookie since #155. If something starts to, the
        right outcome is that the page stops being cached -- not that the
        cookie is cached with it.
      MESSAGE
    end
  end

  # Rails declares its own freshness since #155, so nginx must not override it.
  test 'no cache overrides the lifetime the application declares' do
    cached_rails_blocks.each do |block|
      next if block.match?(LANGUAGE_INDEPENDENT)

      location = block[/location[^{]*/].to_s.strip
      ignored = block.lines.grep(/proxy_ignore_headers/).join

      %w[Cache-Control Expires].each do |header|
        assert_not_includes ignored, header, <<~MESSAGE.chomp
          #{location} ignores #{header}, so the vhost decides the lifetime and
          ApplicationController::FRESHNESS is decoration. The two then drift
          silently, which is how a page the application thinks is good for a
          minute gets held for five.
        MESSAGE
      end
    end
  end

  # Vary covers headers. It says nothing about the query string, and
  # ?locale= still selects a language on the routes that have no prefixed
  # form -- the snapshot routes sit outside `scope "(:locale)"` because
  # positional route helpers break under it, and the Open Wall mosaic links
  # to them with exactly that parameter. A cache that drops the query from
  # its key therefore stores one language under a key that claims none:
  #
  #   /snapshots/3589409?locale=ru  Accept-Language: en  ->  lang="ru"  MISS
  #   /snapshots/3589409            Accept-Language: en  ->  lang="ru"  HIT
  test 'a key that drops the query still accounts for the locale parameter' do
    cached_rails_blocks.each do |block|
      next if block.match?(LANGUAGE_INDEPENDENT)

      location = block[/location[^{]*/].to_s.strip
      key = block[/proxy_cache_key\s+([^;]+);/, 1].to_s

      next if key.include?('$request_uri') # the whole query is in the key already

      assert_includes key, '$locale_key', <<~MESSAGE.chomp
        #{location} keys its cache on #{key}, which drops the query string,
        but ?locale= still changes the language of the page it caches.

        Use $locale_key (conf.d/openipc-microcache.conf), which normalises the
        parameter to the supported set. Keying on $arg_locale raw would let
        ?locale=<anything> fragment the cache without bound, which is the
        flood the $uri key exists to absorb.
      MESSAGE
    end
  end

  test 'the locale key is normalised rather than taken raw' do
    micro = Rails.root.join('deploy/nginx/conf.d/openipc-microcache.conf').read

    assert_match(/map\s+\$arg_locale\s+\$locale_key\s*\{/, micro,
                 '$locale_key is used in a cache key and has to be defined')
    assert_match(/default\s+"";/, micro,
                 'an unrecognised ?locale= must collapse to one bucket, not create its own')
  end

  # Every rule these locations carry -- the firmware rate limit, the
  # microcache, the concurrency caps -- applies only to paths the regex
  # matches. They are anchored at ^/, so a locale prefix walks past all of
  # them. #154 localized /open-wall and that alone opened the gap: /open-wall
  # was microcached and capped, /ru/open-wall was neither. The rest of #154
  # localizes the catalogue, at which point /ru/cameras/.../download_full_image
  # would be the same 1s-of-CPU, 8-32MB-of-disk action with no limit_req in
  # front of it.
  #
  # Named by the route family they guard, never by the locales they list. An
  # earlier version of this test hardcoded `ru|zh` in each pattern, which meant
  # adding a locale could be made to pass by editing the patterns one at a time
  # while two guards stayed stale -- the test agreeing with itself rather than
  # with the vhost.
  GUARDED_ROUTES = ['cameras/vendors', 'snapshots/', '(open-wall'].freeze

  # The optional locale alternation each guarded location actually carries.
  # BOTH vhosts. This file read only the production one until a review pointed
  # out that org.openipc.dev keeps its own copy of the firmware-download
  # location -- which still matched /cameras only, so on dev a localized
  # firmware build had no limit_req in front of it. The environment used to
  # validate a change being the one without the protection is the worst place
  # for that gap to sit.
  VHOSTS = {
    'org.openipc' => VHOST,
    'org.openipc.dev' => Rails.root.join('deploy/nginx/sites-available/org.openipc.dev').read
  }.freeze

  def guard_locales(config)
    GUARDED_ROUTES.filter_map do |route|
      line = config.lines.find { |l| l.include?('location ~ ') && l.include?(route) }
      next if line.nil? # not every location exists in every vhost

      [route, line[/\(\?:\(\?:([a-z|]+)\)/, 1]&.split('|')&.sort]
    end
  end

  test 'a locale prefix cannot walk past the rate limits and caches' do
    VHOSTS.each do |name, config|
      guard_locales(config).each do |route, locales|
        assert_not_nil locales, <<~MESSAGE.chomp
          In #{name}, the location guarding #{route} carries no optional
          locale prefix.

          These regexes are anchored at ^/. Without (?:(?:ru|zh)/)? the rule
          stops applying the moment the route is localized, which for the
          firmware location means an unlimited image build behind /ru/.
        MESSAGE
      end
    end
  end

  # The nginx lists and the Rails list have no connection, and a disagreement
  # is silent in the direction that matters: a locale Rails serves but nginx
  # does not know about is an unguarded path. Checked for every guard, not
  # whichever one appears first in the file.
  test 'the prefixes nginx knows match the locales Rails puts in a path' do
    rails_locales = Multilang::IN_PATH.source.split('|').sort

    VHOSTS.each do |name, config|
      guard_locales(config).each do |route, locales|
        assert_equal rails_locales, locales, <<~MESSAGE.chomp
          In #{name}, the location guarding #{route} knows #{locales.inspect}
          but Rails serves #{rails_locales.inspect} as path prefixes. A locale
          in the second list and not the first is a path with no rate limit,
          no cache and no concurrency cap.
        MESSAGE
      end
    end
  end

  # /snapshots/123ru with no parameter and /snapshots/123?locale=ru built the
  # same key when $locale_key was simply appended, and the first URL resolves:
  # the location regex is not anchored at the end and MySQL casts "123ru" to
  # 123. Reproduced on production -- an English render was served for the
  # Russian page. The bounded field has to be delimited, not concatenated.
  test 'the locale field in a cache key cannot run into the path' do
    cached_rails_blocks.each do |block|
      key = block[/proxy_cache_key\s+([^;]+);/, 1].to_s
      next unless key.include?('$locale_key')

      assert_match(/\|\$locale_key\|/, key, <<~MESSAGE.chomp)
        #{block[/location[^{]*/].to_s.strip} builds its key as #{key}.

        $locale_key must be bracketed by delimiters. Appended straight onto
        the path, /snapshots/123ru and /snapshots/123?locale=ru are the same
        key, and anyone can seed the Russian entry with an English render.
      MESSAGE
    end
  end

  test 'the guard covers the locations that actually render pages' do
    covered = cached_rails_blocks.reject { |b| b.match?(LANGUAGE_INDEPENDENT) }

    assert_operator covered.length, :>=, 2,
                    'expected the Open Wall and per-snapshot caches to be found; the vhost was reshaped'
  end

  # The firmware location is the one that exists in both vhosts, and the one
  # where being unguarded costs a second of CPU and 8-32MB of disk per call.
  test 'the firmware limit is guarded in every vhost that has it' do
    found = VHOSTS.filter_map do |name, config|
      name if config.include?('download_full_image')
    end

    assert_equal VHOSTS.keys.sort, found.sort,
                 'a vhost stopped serving the firmware action, or started and was not guarded'
  end
end
