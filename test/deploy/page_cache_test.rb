# frozen_string_literal: true

require 'test_helper'

# The pages themselves are cached by nginx now (#233).
#
# On 2026-09-20 the front page took 62,588 requests and every one was `cache=-`
# -- every one a Rails render, for 1,787 distinct clients. Assets were at 89%
# hit, snapshots and the Open Wall were cached, and the largest class on the
# site was the only large one that was not.
#
# What makes the catch-all different from every other cached location, and what
# these hold: it serves whatever Rails still answers that nothing else claims,
# so the guards that are right for a gallery page are wrong here, and the ways
# to get it wrong are all one plausible line.
class PageCacheTest < ActiveSupport::TestCase
  VHOST = Rails.root.join('deploy/nginx/sites-available/org.openipc').read.freeze

  # The block that proxies to Rails. It was `location /` until #157 put the
  # static-bundle seam there; `location /` now serves files and falls through
  # to this one, so everything below moved with the proxying and none of it
  # changed. The scan still selects on proxy_pass rather than trusting the
  # name, because the point of the helper is to find the block that talks to
  # Rails whatever it is called next.
  def catch_all
    blocks = VHOST.scan(/\n    location @rails \{\n.*?\n    \}\n/m)
    proxying = blocks.select { |b| b.include?('proxy_pass') }

    assert_equal 1, proxying.length,
                 "expected exactly one proxying `location @rails`, found #{proxying.length} " \
                 "among #{blocks.length} blocks"
    proxying.first
  end

  # Directives only. The block explains at length why it does *not* set
  # proxy_cache_valid and does *not* hide Set-Cookie, and a naive `include?`
  # matches that prose and reports the opposite of the truth -- which is what
  # the first version of these tests did.
  def directives
    catch_all.lines.reject { |l| l.strip.start_with?('#') }.join
  end

  # Directives that belong to a `server` block itself, not to any location
  # inside it -- which is what "inherited" means and what a location's own
  # add_header replaces. Tracked by brace depth rather than indentation, so it
  # does not quietly stop working the day someone reformats the file.
  def server_level_directives
    depth = 0
    location_depth = nil

    VHOST.lines.filter_map do |line|
      keep = depth == 1 && location_depth.nil? && directive?(line)
      depth, location_depth = advance(line, depth, location_depth)
      keep ? line : nil
    end
  end

  def directive?(line)
    stripped = line.strip
    !stripped.empty? && !stripped.start_with?('#') && !stripped.end_with?('{')
  end

  def advance(line, depth, location_depth)
    stripped = line.strip
    opens_location = stripped.end_with?('{') && stripped.start_with?('location', 'if (')
    location_depth = depth if location_depth.nil? && opens_location

    depth += line.count('{') - line.count('}')
    location_depth = nil if location_depth && depth <= location_depth
    [depth, location_depth]
  end

  test 'the page cache exists at all' do
    assert_includes directives, 'proxy_cache openipc_micro',
                    'the largest class of traffic on the site is uncached again'
  end

  # A response carrying Set-Cookie must not be stored, and nginx already
  # refuses to store one. Every other cached location strips the header
  # instead. Here it stays visible: nothing on the site sets a cookie since the
  # admin went (#288), so one appearing is a regression to be seen in a
  # browser, and hiding it would only keep serving whatever set it.
  test 'the fallback does not strip Set-Cookie the way the others do' do
    assert_not_includes directives, 'proxy_hide_header Set-Cookie', <<~MESSAGE.chomp
      `location @rails` hides Set-Cookie. That is correct for the gallery locations,
      and wrong for the catch-all: nothing should be setting a cookie at all
      (#288), and hiding the header turns the first sign of one into silence.

      Not storing such a response is what is wanted, and nginx does that by
      itself.
    MESSAGE
  end

  # Rails is the only thing that knows about the flash, so it is the only thing
  # that can decide what may be cached.
  # A proxy_cache_valid here is a second opinion that overrides the first on
  # exactly the pages where the first one matters.
  test 'nginx does not offer its own opinion on how long a page lives' do
    offered = directives.lines.grep(/proxy_cache_valid/).map(&:strip)

    assert_empty offered, <<~MESSAGE.chomp
      `location @rails` sets a lifetime of its own:

        #{offered.join("\n        ")}

      This location serves every page carrying a flash.
      ApplicationController declares those `max-age=0, private` and nginx
      honours it; proxy_cache_valid applies to responses that say nothing,
      which here are the ones that must not be stored.
    MESSAGE
  end

  # `add_header` in a location replaces every inherited one. The server block
  # sets HSTS, so adding X-Cache-Status without repeating it removes HSTS from
  # every page on the site -- invisibly, because Rails sends a stronger one of
  # its own and a browser would still be protected.
  test 'adding a cache header does not drop the security header above it' do
    server_level = server_level_directives.grep(/add_header Strict-Transport-Security/)
                                          .map(&:strip).uniq

    refute_empty server_level, <<~MESSAGE.chomp
      No `server` block declares HSTS any more.

      This test used to grep the whole file, which the catch-all's own copy of
      the directive satisfied -- so removing the inherited one left every
      other location without HSTS and this test still green. Whatever removed
      it needs to be looked at, not this assertion.
    MESSAGE
    assert_includes directives, server_level.first, <<~MESSAGE.chomp
      `location @rails` uses add_header and does not repeat:

        #{server_level.first}

      add_header at location level replaces all inherited add_header
      directives, so this location would serve every page on the site without
      the vhost's HSTS.
    MESSAGE
  end

  # Both rules existed for the admin alone (#288). The bypass sent anyone with
  # a session cookie past the cache, so a signed-in admin saw the moderation
  # controls rather than an anonymous copy; the no_cache kept an admin's render
  # -- which printed the uploader's IP and MAC -- out of a cache keyed on the
  # path alone. With no admin, nothing sets a cookie and no render differs by
  # who asked, so either rule coming back is a rule for a problem that does not
  # exist, and the bypass would hand any client a free cache-skip by sending
  # a cookie.
  test 'no location keeps a rule for the admin that is gone' do
    %w[org.openipc org.openipc.dev].each do |name|
      vhost = Rails.root.join('deploy/nginx/sites-available', name).read
      live = vhost.lines.reject { |line| line.lstrip.start_with?('#') }.join

      %w[$cookie__openipc_session $upstream_http_x_admin_view X-Admin-View].each do |rule|
        assert_not_includes live, rule, "#{name} still refers to #{rule}, which only the admin needed"
      end
    end
  end

  # Vary covers headers, not the query string. The front page renders per
  # Accept-Language -- `/` with Accept-Language: ru answers lang="ru" -- so
  # Vary is load-bearing here and nginx honours it. The query has to be in the
  # key by hand, and dropping it is what produced the bug the microcache
  # configuration documents.
  test 'the key keeps the query string' do
    key = directives[/proxy_cache_key (.*);/, 1]

    assert key, '`location @rails` caches without a key of its own'
    assert_includes key, '$request_uri', <<~MESSAGE.chomp
      The key is `#{key}`, which drops the query string. ?locale= still
      selects a language on routes with no prefixed form, so a key that
      ignores it stores one language under a key that claims none.
    MESSAGE
  end
end
