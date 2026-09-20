# frozen_string_literal: true

require 'test_helper'

# The property the whole migration rests on: for a public page, the URL
# determines the response. Until #155 it did not -- every response carried
# Set-Cookie because the locale was kept in the session and a CSRF token was
# minted for pages with no form, and a response carrying Set-Cookie is one most
# caches decline to store. That is why `location /` in the vhost has no cache
# at all and 51% of requests reached Rails for pages that had not changed.
#
# #154 made the address carry the language. This file is what stops the cookie
# coming back.
class CacheabilityTest < ActionDispatch::IntegrationTest
  include Devise::Test::IntegrationHelpers

  # Forgery protection is off in the test environment, which makes
  # csrf_meta_tags render nothing -- so every assertion here about cookies and
  # tokens passed whether or not the layout emitted one. Found by reverting
  # the change and watching the test still pass, which is the only way that
  # kind of hole shows up.
  #
  # Turned on for this file alone: it is the one file whose subject is what
  # the response headers look like in production.
  setup do
    @forgery_protection = ActionController::Base.allow_forgery_protection
    ActionController::Base.allow_forgery_protection = true
  end

  teardown { ActionController::Base.allow_forgery_protection = @forgery_protection }

  PUBLIC = ['/', '/ru', '/zh', '/donate', '/ru/donate',
            '/supported-hardware/featured', '/open-wall'].freeze

  test 'no public page sets a cookie' do
    offenders = PUBLIC.filter_map do |path|
      get path
      cookie = response.headers['Set-Cookie']
      "#{path} -> #{cookie}" if cookie.present?
    end

    assert_empty offenders, <<~MESSAGE.chomp
      These public pages still set a cookie:

      #{offenders.map { |o| "  #{o}" }.join("\n")}

      A response carrying Set-Cookie cannot be stored by a shared cache. Find
      what wrote to the session -- the usual causes are a session[] assignment
      and csrf_meta_tags being rendered on a page with no form.
    MESSAGE
  end

  test 'no public page varies by cookie' do
    PUBLIC.each do |path|
      get path

      assert_not_includes response.headers['Vary'].to_s, 'Cookie',
                          "#{path} varies by cookie, so it cannot be shared between visitors"
    end
  end

  test 'public pages say how long they are good for' do
    PUBLIC.each do |path|
      get path
      cache_control = response.headers['Cache-Control'].to_s

      assert_includes cache_control, 'public', "#{path} is not declared public"
      assert_match(/max-age=[1-9]/, cache_control, "#{path} has no positive max-age")
      assert_match(/stale-while-revalidate=[1-9]/, cache_control,
                   "#{path} cannot be served stale while it refreshes, which is what a flood needs")
    end
  end

  # The half that must NOT change. An admin sees uploader IP and MAC addresses
  # on URLs an anonymous visitor also reaches.
  test 'the admin area is never publicly cacheable' do
    get '/admin/sign_in'

    assert_not_includes response.headers['Cache-Control'].to_s, 'public',
                        'the sign-in page is declared publicly cacheable'
  end

  test 'a signed-in admin gets no public freshness and is marked uncacheable' do
    sign_in admins(:one)
    get '/open-wall'

    assert_equal '1', response.headers['X-Admin-View'],
                 'nginx has no other way to know this response was rendered for an admin'
    assert_not_includes response.headers['Cache-Control'].to_s, 'public'
  end

  # The wizard is the one public page with a form, so it is the one that still
  # needs a token -- and the token is what puts the cookie back, which is why
  # #156 turns its PUT into a GET.
  test 'the token is emitted where a form needs it, and nowhere else' do
    get '/supported-hardware/featured'
    assert_select 'meta[name=csrf-token]', false,
                  'a page with no form is minting a CSRF token, which writes the session'

    get '/open-wall'
    assert_select 'meta[name=csrf-token]', false
  end

  # The wizard sets a cookie because its form needs a token, so it must not be
  # declared publicly cacheable -- nginx would refuse to store it anyway, but
  # a CDN or any other cache that believes the header would hand one visitor's
  # session to the next. Found on dev: it was answering
  # `public, max-age=3600` alongside Set-Cookie.
  test 'a page that sets a cookie is never declared publicly cacheable' do
    vendor = Vendor.find_by(name: 'Cache Test Vendor') || Vendor.create!(name: 'Cache Test Vendor')
    soc = Soc.find_by(model: 'CT1000') ||
          Soc.create!(model: 'CT1000', vendor: vendor, family: 'ct', status: 'done',
                      uboot_filename: 'u.bin', linux_filename: 'l.bin')

    get "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}"

    assert_response :success
    assert_not_includes response.headers['Cache-Control'].to_s, 'public',
                        'this page mints a CSRF token, so its response carries Set-Cookie'
  end

  # The other direction, and the one that breaks a feature rather than a cache
  # if it is wrong: the wizard posts, so it must still get a token.
  test 'the wizard still gets its token' do
    # Built, not looked up: this database is regenerated from development and
    # carries no vendors or SoCs, so a test that skips on an empty table never
    # runs and proves nothing.
    vendor = Vendor.find_by(name: 'Cache Test Vendor') || Vendor.create!(name: 'Cache Test Vendor')
    soc = Soc.find_by(model: 'CT1000') ||
          Soc.create!(model: 'CT1000', vendor: vendor, family: 'ct', status: 'done',
                      uboot_filename: 'u.bin', linux_filename: 'l.bin')

    get "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}"

    assert_response :success
    assert_select 'meta[name=csrf-token]', 1,
                  'the wizard form would fail every submission with InvalidAuthenticityToken'
  end
end
