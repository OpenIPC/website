# frozen_string_literal: true

require 'test_helper'

# The pre-relaunch URLs. Every one of these is in search results, in forum posts
# and in the wiki, and none of that is ours to edit -- so the map is code, and
# the code is tested. Without this, a rename quietly turns an inbound link into
# a 302 to the homepage, which looks like it works.
class RedirectsTest < ActionDispatch::IntegrationTest
  PERMANENT = {
    '/home' => '/',
    '/introduction' => '/',
    '/aaa' => '/',
    '/fpv' => '/low-latency',
    '/our-projects' => '/ecosystem',
    '/our-software' => '/ecosystem',
    '/our-channels' => '/community',
    '/support-open-source' => '/donate',
    '/sponsor' => '/donate',
    '/hardware' => '/supported-hardware/featured',
    '/SDK' => '/supported-hardware',
    '/supported-hardware' => '/supported-hardware/featured'
  }.freeze

  PERMANENT.each do |from, to|
    test "301 #{from} -> #{to}" do
      get from

      assert_response :moved_permanently
      assert_equal to, URI.parse(response.location).path
    end
  end

  # 302, not 301: /about is meant to become a page of its own. A 301 is cached
  # by browsers indefinitely and would outlive the decision.
  test '302 /about -> /community, until the About page ships' do
    get '/about'

    assert_response :found
    assert_equal '/community', URI.parse(response.location).path
  end

  # Each redirect has to land somewhere real. A target that itself falls through
  # the catch-all would still answer 301 here and 302-then-200 to a visitor.
  test 'every redirect target resolves' do
    (PERMANENT.values + ['/community']).uniq.each do |target|
      get target
      follow_redirect! while response.redirect?

      assert_response :success, "#{target} does not resolve"
    end
  end

  # /tools/bandwidth-calculator routed to pages#bandwidth_calculator, an action
  # that has never existed -- no method, no template -- so it raised
  # AbstractController::ActionNotFound and answered 500 in production. The route
  # is gone; the catch-all sends the visitor to the homepage, which is a page.
  test 'the bandwidth calculator URL no longer raises' do
    get '/tools/bandwidth-calculator'

    assert_redirected_to '/'
  end

  # The camera upload API posts to /snapshots. Nothing in the relaunch may
  # shadow it -- the gallery moved to /open-wall, the API did not move at all.
  test 'cameras can still POST to /snapshots' do
    post '/snapshots'

    assert_not response.redirect?, 'the camera upload API was shadowed by a redirect'
    # Not 500: SnapshotsController#create answers an empty upload with 415, and
    # accepting a server error here would let a controller regression pass as
    # though the API still worked.
    assert_includes [400, 415, 422], response.status,
                    "expected a request error for an empty upload, got #{response.status}"
  end

  test '/open-wall is the gallery, not a fall-through to the homepage' do
    get '/open-wall'

    assert_response :success
  end

  # These send visitors off to GitHub, so a typo here is a dead end nobody on
  # this side sees. One of them spelled the organisation `openipc//coupler`,
  # with the wrong case and a doubled slash, and `/telemetry` pointed at a
  # repository that has never existed.
  GITHUB_REDIRECTS = %w[coupler firmware ipctool microbe-web smolrtsp yaml-cli wiki].freeze

  test 'every GitHub shortcut points at a well-formed OpenIPC repository' do
    GITHUB_REDIRECTS.each do |repo|
      ['', '/some/deep/path'].each do |suffix|
        get "/#{repo}#{suffix}"

        assert_response :redirect, "/#{repo}#{suffix} does not redirect"
        assert_match %r{\Ahttps://github\.com/OpenIPC/#{Regexp.escape(repo)}/?\z},
                     response.location,
                     "/#{repo}#{suffix} sends visitors to #{response.location}"
      end
    end
  end

  # The repository behind it does not exist, so bouncing anyone to GitHub's own
  # 404 was worse than answering here. Deleting the route would be worse still:
  # unknown paths 302 to the homepage, which claims the page moved there.
  test '/telemetry says it is gone rather than pretending it moved' do
    ['/telemetry', '/telemetry/anything'].each do |path|
      get path

      assert_response :gone, "#{path} answered #{response.status}"
    end
  end
end
