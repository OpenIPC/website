# frozen_string_literal: true

require 'test_helper'

# The mirrors are half of a contract with the origin, and until now only the
# origin's half was in the repository.
#
# openipc.kz and openipc.cloud moved to a host this project administers
# (deploy/nginx/mirrors/kz/), so that half is here too and can be held to the
# other. Three things have to agree across the two, and each of them fails
# silently:
#
#   - the origin trusts the mirror's address, or every reader behind it
#     collapses onto one address in the logs and in every limit_conn (#145);
#   - the mirror appends X-Forwarded-For rather than overwriting it, which is
#     what makes `real_ip_recursive on` able to skip the mirrors and find the
#     person;
#   - the mirror carries the WebSocket upgrade, or the Open Wall shows test
#     cards to everyone behind it and nothing appears in any log.
class MirrorConfigTest < ActiveSupport::TestCase
  ORIGIN = Rails.root.join('deploy/nginx/nginx.conf').read.freeze
  MIRROR = Rails.root.join('deploy/nginx/mirrors/kz').freeze
  VHOSTS = %w[kz.openipc cloud.openipc].freeze

  TRUSTED = ORIGIN.scan(/^\s*set_real_ip_from\s+(\S+?);/).flatten.freeze

  test 'the origin trusts the host that serves openipc.kz and openipc.cloud' do
    assert_includes TRUSTED, '194.238.42.216',
                    'the mirror sends X-Forwarded-For and the origin ignores it unless the ' \
                    'address is trusted here; untrusted, 17,000 readers a day are one address'
  end

  test 'the origin still trusts the other mirrors' do
    assert_includes TRUSTED, '194.58.109.202', 'openipc.ru, опенипц.рф'
  end

  test 'forwarded addresses are read, and read recursively' do
    assert_match(/^\s*real_ip_header\s+X-Forwarded-For;/, ORIGIN)
    # The mirrors proxy to each other, so the last entry is often another
    # mirror rather than the reader.
    assert_match(/^\s*real_ip_recursive\s+on;/, ORIGIN)
  end

  test 'the mirror appends the reader rather than overwriting' do
    proxy = MIRROR.join('snippets/openipc-mirror.conf').read

    assert_equal 2, proxy.scan(/X-Forwarded-For\s+\$proxy_add_x_forwarded_for;/).size,
                 'both locations, or the one that is missing it hands the origin a ' \
                 'chain with a hole in it'
    refute_match(/X-Forwarded-For\s+\$remote_addr;/, proxy,
                 'overwriting drops the chain the origin walks')
  end

  test 'the mirror carries the WebSocket upgrade' do
    proxy = MIRROR.join('snippets/openipc-mirror.conf').read
    upgrade = proxy[/location \^~ \/api\/v1\/wall\/ \{.*?\n\}/m]

    assert upgrade, 'the wall channel has no location of its own'
    assert_match(/proxy_http_version\s+1\.1;/, upgrade,
                 'HTTP/1.0 cannot carry an Upgrade and nginx uses it by default')
    assert_match(/proxy_set_header Upgrade\s+\$http_upgrade;/, upgrade)
    assert_match(/proxy_set_header Connection\s+\$connection_upgrade;/, upgrade)
    assert_match(/map \$http_upgrade \$connection_upgrade/,
                 MIRROR.join('conf.d/openipc-upgrade.conf').read,
                 'the map that variable comes from has to be at http level')
  end

  # A socket carries frames for as long as the reader has the page open, and
  # the default read timeout is 60 seconds.
  test 'the mirror does not time the socket out' do
    proxy = MIRROR.join('snippets/openipc-mirror.conf').read
    upgrade = proxy[/location \^~ \/api\/v1\/wall\/ \{.*?\n\}/m]

    assert_match(/proxy_read_timeout\s+1h;/, upgrade)
    assert_match(/proxy_buffering\s+off;/, upgrade)
  end

  VHOSTS.each do |vhost|
    test "#{vhost} keeps the origin's upload limit" do
      # Cameras POST snapshots through the mirrors. org.openipc sets no
      # client_max_body_size, so the origin's limit is nginx's default.
      assert_match(/client_max_body_size 1m;/, MIRROR.join("sites-available/#{vhost}").read)
    end

    test "#{vhost} redirects plain HTTP and verifies the origin's certificate" do
      config = MIRROR.join("sites-available/#{vhost}").read

      assert_match(%r{return 301 https://\$host\$request_uri;}, config)
      assert_match(/include snippets\/openipc-mirror\.conf;/, config,
                   'the proxy itself is shared, so the two names cannot drift apart')
    end
  end

  test 'the origin certificate is verified, not merely named' do
    proxy = MIRROR.join('snippets/openipc-mirror.conf').read

    assert_equal 2, proxy.scan(/proxy_ssl_verify\s+on;/).size
    assert_equal 2, proxy.scan(/proxy_ssl_name\s+openipc\.org;/).size
  end
end
