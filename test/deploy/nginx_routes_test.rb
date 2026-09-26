# frozen_string_literal: true

require 'test_helper'

# nginx answers the router's redirects, retirements and catch-all itself (#302),
# from deploy/nginx/conf.d/openipc-redirects.conf -- which is generated from
# config/routes.rb and must never drift from it. deploy/nginx/check-config.sh
# --seam proves what nginx then does with it; this proves the file is the
# router's.
class NginxRoutesTest < ActiveSupport::TestCase
  GENERATOR = Rails.root.join('deploy/nginx/generate_routes.rb')

  setup do
    ENV['NGINX_ROUTES_LIBRARY'] = '1'
    load GENERATOR.to_s
  ensure
    ENV.delete('NGINX_ROUTES_LIBRARY')
  end

  test 'the committed map is what the router generates' do
    assert_equal NginxRoutesConf.render, NginxRoutes::OUT.read,
                 'deploy/nginx/conf.d/openipc-redirects.conf is stale: ' \
                 'bin/rails runner deploy/nginx/generate_routes.rb'
  end

  # The traps the issue names, read back out of what the router generates
  # rather than restated as a second map.
  test 'a kept query string travels, a plain redirect drops it, and /about stays temporary' do
    entries = NginxRoutes.entries.to_h { |e| [e.source, [e.action, e.target]] }

    assert_equal ['301', '/$is_args$args'], entries['GET /home(.:format)']
    assert_equal ['301', '/low-latency$is_args$args'], entries['GET /fpv(.:format)']
    assert_equal ['302', '/community$is_args$args'], entries['GET /about(.:format)']
    assert_equal ['301', '/supported-hardware/featured'], entries['GET /hardware(.:format)']
  end

  test 'the locale prefix travels where the router carried it' do
    targets = NginxRoutes.entries.select { |e| e.source == 'GET (/:locale)/supported-hardware(.:format)' }

    assert_equal ['/$1/supported-hardware/featured', '/supported-hardware/featured'], targets.map(&:target)
    assert_match %r{/\(ru\|zh\)/supported}, targets.first.key
  end

  test 'retired addresses answer 410 to every method, and pages stay the application\'s' do
    entries = NginxRoutes.entries

    %w[/binaries /telemetry /merchandise /admin /rails/active_storage].each do |path|
      entry = entries.find { |e| e.source.include?(" #{path}") || e.source.include?(")#{path}") }
      assert entry, "no entry for #{path}"
      assert_equal '410', entry.action, path
      assert entry.key.start_with?('~^[A-Z]+ '), "#{path} is retired for GET only"
    end
    donate = entries.select { |e| e.source.include?('/donate(') }
    assert donate.all? { |e| e.action == 'rails' }, 'a page the application renders was taken from it'
  end

  test 'the upload is not answered by the map' do
    post = NginxRoutes.entries.select { |e| e.source.start_with?('POST ') && e.source.include?('/snapshots(') }

    refute_empty post
    assert post.all? { |e| e.action == 'rails' }, 'POST /snapshots must reach an application'
  end
end
