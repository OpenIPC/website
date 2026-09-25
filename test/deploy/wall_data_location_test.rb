# frozen_string_literal: true

require 'test_helper'

# The wall's JSON addresses must not inherit the socket's nginx block.
#
# `/api/v1/wall/` held one prefix location, written for the frame channel: no
# caching, no rate limit, no buffering, an hour of read timeout. All of that is
# right for a connection. #165 then put four documents under the same prefix --
# the wall page, the snapshot, the archive and the slideshow -- and a prefix
# location covers everything beneath it, so each of them would have reached
# Rails on every request and minted a WallGrant outside the per-address limit
# every wall page has had since #146. Review on #281 found it before it shipped.
#
# The split is by prefix length: `/api/v1/wall/cable` is longer than
# `/api/v1/wall/`, and nginx takes the longest matching prefix whatever the
# order in the file. Both halves are asserted here because neither is visible
# in any other test, and because the failure is silent -- everything still
# works, it just works without the controls.
class WallDataLocationTest < ActiveSupport::TestCase
  VHOSTS = {
    'production' => 'deploy/nginx/sites-available/org.openipc',
    'dev' => 'deploy/nginx/sites-available/org.openipc.dev'
  }.freeze

  # The block a location opens, up to the closing brace at its own indentation.
  def location(vhost, prefix)
    vhost[/^    location \^~ #{Regexp.escape(prefix)} \{.*?\n    \}/m]
  end

  VHOSTS.each do |env, path|
    test "the #{env} socket block covers the socket and nothing else" do
      vhost = Rails.root.join(path).read
      cable = location(vhost, '/api/v1/wall/cable')

      assert cable, 'the frame channel has no location of its own'
      assert_match(/proxy_no_cache 1;/, cable, 'a socket has nothing to replay')
      assert_match(/proxy_buffering\s+off;/, cable)
      refute_match(/^    location \^~ \/api\/v1\/wall\/ \{[^}]*proxy_read_timeout\s+1h;/m, vhost,
                   'the wall-wide location carries the socket settings, so every JSON ' \
                   'address under it is uncached, unthrottled and unbuffered')
    end

    test "the #{env} wall data is rate-limited" do
      data = location(Rails.root.join(path).read, '/api/v1/wall/')

      assert data, 'the wall JSON addresses have no location of their own'
      assert_match(/limit_req zone=snapshot_pages/, data,
                   'every response here carries a grant; without the wall\'s own ' \
                   'per-address limit they can be collected as fast as Rails will render')
      assert_match(/limit_req_status 429;/, data)
    end

    test "the #{env} wall data is microcached, on a key that separates its addresses" do
      data = location(Rails.root.join(path).read, '/api/v1/wall/')

      assert_match(/proxy_cache openipc_micro;/, data)
      # $uri, or one address's body -- and its grant -- is served for another.
      assert_match(/proxy_cache_key .*\$uri;/, data)
      assert_match(/proxy_cache_valid 200 60s;/, data)
    end
  end
end
