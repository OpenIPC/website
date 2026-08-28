# frozen_string_literal: true

require 'test_helper'

# The seven pages the relaunch adds. They answer on their own URLs but nothing
# links to them yet, so nobody would notice one of them 500ing -- which is
# exactly why they are tested before the cutover rather than after it.
class RelaunchPagesTest < ActionDispatch::IntegrationTest
  PAGES = {
    '/' => 'home',
    '/get-started' => 'get_started',
    '/low-latency' => 'low_latency',
    '/ecosystem' => 'ecosystem',
    '/business' => 'business',
    '/community' => 'community',
    '/donate' => 'donate'
  }.freeze

  LOCALES = %i[en ru zh].freeze

  PAGES.each do |path, key|
    LOCALES.each do |locale|
      test "#{path} renders in #{locale}" do
        get "#{path}?locale=#{locale}"

        assert_response :success
        # A key that exists in en but not here would render as this span, and
        # the page would still answer 200. i18n-tasks catches a key missing
        # everywhere; only rendering catches one missing from one locale.
        assert_no_match(/translation missing/i, response.body)
        assert_select 'h1, h2', minimum: 1
      end
    end

    test "#{path} sets a page title" do
      get path

      assert_select 'title' do |tags|
        assert_no_match(/translation missing/i, tags.first.text)
        assert_match(/OpenIPC/, tags.first.text)
      end
      assert_not_nil I18n.t("pages.#{key}.title", default: nil), "pages.#{key}.title is not defined"
    end
  end

  # A fresh checkout has an empty database. The homepage reads counts and
  # snapshots from it, and must render rather than 500 or show "0 supported
  # SoCs" as though that were a fact about the project.
  test 'the homepage renders against an empty database' do
    assert_equal 0, Snapshot.count
    assert_equal 0, Soc.count

    get '/'

    assert_response :success
  end

  # These pages lay out their own full-bleed sections and opt out of the
  # layout's wrapper. If the switch stopped working they would still render,
  # just wrongly, inside a centred column.
  #
  # The selector is the layout's own `container mb-4`, not any `.container`:
  # most of these pages open a plain `.container` of their own directly under
  # <main>, and matching that would pass whether the switch worked or not.
  test 'the relaunch pages opt out of the layout wrapper' do
    PAGES.each_key do |path|
      get path

      assert_empty css_select('main > div.container.mb-4'), "#{path} kept the layout wrapper"
    end
  end

  # The inverse, so the switch is pinned from both sides: a page that asks for
  # nothing must still get the wrapper.
  test 'a page that does not ask for full width still gets the layout wrapper' do
    get '/our-team'

    assert_not_empty css_select('main > div.container.mb-4')
  end

  # Every internal link on the new pages has to resolve. A link to a path the
  # router does not know falls through the catch-all to a 302 home, which looks
  # like a working link right up until someone clicks it.
  test 'internal links on the relaunch pages all resolve' do
    PAGES.each_key do |path|
      get path

      hrefs = css_select('a[href^="/"]').map { |a| a['href'] }.uniq
      hrefs.each do |href|
        get href
        # A redirect is fine -- /supported-hardware legitimately 301s to
        # /featured. What is not fine is landing at the homepage, which is
        # where the "*unmatched" catch-all sends anything the router does not
        # recognise: the signature of a link to a path that does not exist.
        if response.redirect?
          assert_not_equal '/', URI.parse(response.location).path,
                           "#{path} links to #{href}, which falls through to the catch-all"
          follow_redirect!
        end

        assert_response :success, "#{path} links to #{href}, which answered #{response.status}"
      end
    end
  end

  # The link test above proves that whatever the homepage links resolves. It
  # cannot notice a link that stopped being rendered at all -- a pillar card
  # losing its href, or a section being dropped in a refactor -- so the
  # destinations the homepage is *for* are named here explicitly.
  test 'the homepage links every place it is supposed to send people' do
    get '/'

    %w[/get-started /low-latency /ecosystem /business /supported-hardware /open-wall /donate].each do |path|
      assert_not_empty css_select(%(a[href="#{path}"])), "the homepage no longer links #{path}"
    end
  end

  # The integrator wall is territory-specific: these companies serve Russia and
  # were explicitly not to be shown to everyone.
  test 'Russian integrators appear for ru and for nobody else' do
    marker = PagesHelper::RU_INTEGRATORS.first[:img]

    get '/?locale=ru'

    assert_includes response.body, marker.sub('.png', '')

    %w[en zh].each do |locale|
      get "/?locale=#{locale}"

      assert_not_includes response.body, marker.sub('.png', ''),
                          "RU integrators leaked into #{locale}"
    end
  end

  test 'every partner logo the helper names exists as an asset' do
    (PagesHelper::INTERNATIONAL_PARTNERS + PagesHelper::RU_INTEGRATORS).each do |logo|
      assert_path_exists Rails.root.join('app/assets/images', logo[:img])
    end
  end
end
