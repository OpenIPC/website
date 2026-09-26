# frozen_string_literal: true

require_relative 'conformance_helper'

# The addresses Rails still answers that are not the upload (#291): the
# redirects, the retirements, the catch-all, the availability feed and the
# sitemap. #302 moves the redirects into nginx and #303 the sitemap into the
# static build; these hold whichever side answers, which is the point.
#
# Ported from redirects_test and retired_routes_test, where they talked to the
# app in-process. Paths only in the Location comparisons: the application
# answers relative or absolute depending on who built the redirect, and a
# browser treats the two the same.
class ReadSurfaceConformanceTest < Conformance::Case
  surface :read

  PERMANENT = {
    '/home' => '/', '/introduction' => '/', '/aaa' => '/', '/fpv' => '/low-latency',
    '/our-projects' => '/ecosystem', '/our-software' => '/ecosystem', '/our-channels' => '/community',
    '/support-open-source' => '/donate', '/sponsor' => '/donate',
    '/hardware' => '/supported-hardware/featured', '/SDK' => '/supported-hardware',
    '/supported-hardware' => '/supported-hardware/featured'
  }.freeze

  GONE = ['/binaries', '/binaries.json', '/telemetry', '/telemetry/anything', '/merchandise',
          '/ru/merchandise', '/zh/merchandise', '/snapshots/12345'].freeze

  GITHUB = %w[coupler firmware ipctool microbe-web smolrtsp yaml-cli wiki].freeze

  def location_path(response)
    URI(response['Location']).path
  end

  test 'the old addresses are permanent redirects to their pages' do
    wrong = PERMANENT.filter_map do |from, to|
      response = get(from)
      next if response.code == '301' && location_path(response) == to

      "#{from}: want 301 #{to}, got #{response.code} #{response['Location']}"
    end

    assert_empty wrong
  end

  # 302, not 301: /about is meant to become a page of its own, and a browser
  # caches a 301 indefinitely.
  test '/about is a temporary redirect to /community' do
    response = get('/about')

    assert_equal '302', response.code
    assert_equal '/community', location_path(response)
  end

  test 'what was retired says it is gone' do
    wrong = GONE.filter_map do |path|
      code = get(path).code
      "#{path}: #{code}" unless code == '410'
    end

    assert_empty wrong, 'a retired address that answers anything but 410 tells a crawler it moved'
  end

  # The camera's newest frame as a file, retired in #235. nginx refuses it
  # before Rails hears of it; Rails alone would run the catch-all. So this one
  # holds only through the vhost, and is the example of a behaviour a port
  # does not have to carry because it is not the application's.
  test 'through nginx, a camera frame by URL is gone' do
    skip 'the vhost answers this, not the application' if Conformance.direct?

    assert_equal '410', get('/open-wall/camera/0123456789abcdef.jpg').code
  end

  test 'every GitHub shortcut points at its OpenIPC repository' do
    wrong = GITHUB.product(['', '/some/deep/path']).filter_map do |repo, suffix|
      response = get("/#{repo}#{suffix}")
      next if response.code.start_with?('3') &&
              response['Location'].to_s.match?(%r{\Ahttps://github\.com/OpenIPC/#{Regexp.escape(repo)}/?\z})

      "/#{repo}#{suffix}: #{response.code} #{response['Location']}"
    end

    assert_empty wrong
  end

  test 'an unknown address goes to the home page, and sets nothing' do
    response = get('/no-such-page-at-all', headers: { 'Referer' => 'https://example.test/private' })

    assert_equal '302', response.code
    assert_equal '/', location_path(response)
  end

  # What 378 prerendered pages read on load to say what a visitor can do with
  # each chip (#162). The states are the page's vocabulary; a fourth one is a
  # page that renders nothing for it.
  test 'the availability feed names every chip with a state the pages know' do
    response = get('/api/v1/hardware/availability.json')

    assert_equal '200', response.code
    assert_match %r{\Aapplication/json}, response['Content-Type']
    body = JSON.parse(response.body)
    assert_kind_of String, body['generated_at']
    assert_operator body['socs'].size, :>, 100, 'the feed lost most of the catalogue'
    assert_empty(body['socs'].values.uniq - %w[wizard firmware_only none])
    assert_equal body['socs'].keys.sort, body['socs'].keys, 'the feed is ordered by slug'
    assert_nil response['Set-Cookie']
  end

  test 'the sitemap is XML and offers the catalogue in three languages' do
    response = get('/sitemap.xml')

    assert_equal '200', response.code
    assert_match %r{\A(application|text)/xml}, response['Content-Type']
    assert_includes response.body, '<urlset'
    %w[en ru zh x-default].each do |code|
      assert_includes response.body, %(hreflang="#{code}"), "no #{code} alternates"
    end
    assert_match %r{/cameras/vendors/[a-z0-9._-]+/socs/[a-z0-9._-]+</loc>}, response.body
  end

  test 'the health check answers' do
    assert_equal '200', get('/up').code
  end
end
