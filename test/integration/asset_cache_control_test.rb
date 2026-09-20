# frozen_string_literal: true

require 'test_helper'

# A fingerprinted asset used to answer with `last-modified` and nothing else --
# no Cache-Control, no ETag -- so every repeat visitor revalidated every
# stylesheet, script and font on every page: 27,676 asset requests a day
# against roughly 1,600 humans (#149).
#
# These run against the middleware directly rather than through an integration
# request, because ActionDispatch::Static only serves files when
# config.public_file_server.enabled is on, which in this environment it is not.
# What is under test is the decision, not Static.
class AssetCacheControlTest < ActiveSupport::TestCase
  def respond(path, status: 200, headers: {})
    app = ->(_env) { [status, headers.dup, ['body']] }
    AssetCacheControl.new(app).call('PATH_INFO' => path)
  end

  def cache_control_for(path, **)
    respond(path, **)[1]['Cache-Control']
  end

  test 'a fingerprinted asset may be kept without revalidating' do
    assert_equal AssetCacheControl::IMMUTABLE,
                 cache_control_for('/assets/application-abc123.css')
  end

  # /fonts/ is copied out of node_modules under its own name because public/
  # does not go through Sprockets, so the URL survives a version bump and
  # `immutable` would make replacing a font a year-long commitment.
  test 'a font is cacheable for a month but not immutable' do
    value = cache_control_for('/fonts/ibm-plex-sans-latin-400-normal.woff2')

    assert_equal AssetCacheControl::REVALIDATED_MONTHLY, value
    assert_no_match(/immutable/, value,
                    'public/fonts is not fingerprinted -- see tools/copy-fonts.mjs')
  end

  # The one-liner this replaces, config.public_file_server.headers, applies to
  # everything under public/. Telling the world to keep robots.txt for a year,
  # immutably, is not a change anyone can take back inside a year.
  test 'nothing else under public gets a long cache' do
    ['/robots.txt', '/404.html', '/favicon.ico', '/og-default.png', '/'].each do |path|
      assert_nil cache_control_for(path),
                 "#{path} must not be given an asset cache header"
    end
  end

  # A deploy that briefly races the digest manifest would otherwise pin the
  # miss into every cache between here and the visitor.
  test 'a missing asset is not cached' do
    assert_nil cache_control_for('/assets/gone-abc123.css', status: 404)
    assert_nil cache_control_for('/assets/gone-abc123.css', status: 500)
  end

  test 'a revalidated asset keeps the header' do
    assert_equal AssetCacheControl::IMMUTABLE,
                 cache_control_for('/assets/application-abc123.css', status: 304)
  end

  test 'it replaces whatever came before rather than appending' do
    value = cache_control_for('/assets/application-abc123.css',
                              headers: { 'Cache-Control' => 'no-store' })

    assert_equal AssetCacheControl::IMMUTABLE, value
  end

  # Positioned against Rack::Sendfile rather than ActionDispatch::Static,
  # because Static is only in the stack when public_file_server.enabled is
  # true. It is not during the image build's assets:precompile, where
  # insert_before ActionDispatch::Static aborts the build outright -- which is
  # how this was found, in CI rather than here.
  test 'the middleware sits where it can see what Static served' do
    stack = Rails.application.config.middleware.map(&:name)

    assert_includes stack, 'AssetCacheControl'
    assert_operator stack.index('Rack::Sendfile'), :<, stack.index('AssetCacheControl'),
                    'Rack::Sendfile is the unconditional anchor this is positioned against'

    static = stack.index('ActionDispatch::Static')
    return if static.nil? # not enabled in this environment; nothing to order against

    assert_operator stack.index('AssetCacheControl'), :<, static,
                    'outside Static, or it never sees the response Static produced'
  end
end
