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

  # /get-started shows a command to paste into a root shell on the camera. It
  # has to reach the page whole -- the URL was being clipped by CSS, and the
  # tempting fix is to shorten the command rather than let it wrap.
  test 'the ipctool command reaches the page complete' do
    get '/get-started'

    block = css_select('#ipctool-cmd').first

    assert_not_nil block, 'the terminal block is gone'
    command = block.text
    assert_includes command, 'https://github.com/OpenIPC/ipctool/releases/download/latest/ipctool'
    assert_includes command, 'chmod +x /tmp/ipctool'
    # The copy button copies this element, so it must name it correctly.
    assert_not_empty css_select('[data-copy-target="#ipctool-cmd"]'),
                     'nothing on the page copies the command'
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

  # The radio-link card names devourer, OpenIPC's own userspace Realtek driver,
  # rather than wfb-ng. wfb-ng keeps its place in the credits -- the link exists
  # because of both -- so the test checks the card, not the whole page.
  test 'the radio link card names devourer and links it' do
    get '/low-latency'

    card = css_select('.card').find { |c| c.text.include?('devourer') }
    assert_not_nil card, 'no card on the page names devourer'
    assert_not_empty card.css('a[href="https://github.com/OpenIPC/devourer"]'),
                     'devourer is named but not linked'
    assert_includes response.body, I18n.t('pages.low_latency.credits_text'),
                     'the credits no longer thank both projects'
  end

  # The latency table on /low-latency was unchanged 2022 announcement copy. It
  # was keyed on resolution -- which is very nearly free -- and a 2026 audit of
  # the OpenIPC and wfb-ng chat archives found it optimistic by 40-160 ms at the
  # exact configurations it named, while understating the floor by half. It now
  # compares receive paths, which is what actually decides the number.
  test 'the low-latency page compares receive paths, not resolutions' do
    get '/low-latency'

    assert_response :success
    PagesHelper::LATENCY_PATHS.each do |path|
      assert_includes response.body, I18n.t("pages.low_latency.latency_path_#{path[:key]}"),
                      "the #{path[:key]} path is missing"
      assert_includes response.body, "#{path[:low]}–#{path[:high]}",
                      "the #{path[:key]} figure is missing"
    end
    ['~60 ms', '~80 ms', '~100 ms'].each do |stale|
      assert_not_includes response.body, stale, "the 2022 figure #{stale} is back on the page"
    end
    assert_includes response.body, 'about 30 ms', 'the hero no longer states the real floor'
  end

  # Sighted readers get the unit once, under the axis. A screen reader reaches
  # the numbers one at a time, so each has to carry it -- and the axis and its
  # unit, being decoration for those numbers, must not be read out twice.
  test 'every latency figure says what unit it is in' do
    get '/low-latency'

    unit = I18n.t('pages.low_latency.latency_axis_unit')
    values = css_select('.latency-bars__value')

    assert_equal PagesHelper::LATENCY_PATHS.size, values.size
    values.each do |value|
      assert_includes value.text, unit, "#{value.text.strip} does not say what unit it is in"
    end
    assert_equal 'true', css_select('.latency-bars__unit').first['aria-hidden']
    assert_equal 'true', css_select('.latency-bars__axis').first['aria-hidden']
    css_select('.latency-bars__track').each do |track|
      assert_equal 'true', track['aria-hidden'], 'a bar is read out as if it were content'
    end
  end

  # The bars are positioned by inline percentages against a fixed scale. A
  # figure edited past that scale would render a bar running off the end of its
  # track, which no test of the copy would notice.
  test 'every latency bar fits the scale it is drawn against' do
    PagesHelper::LATENCY_PATHS.each do |path|
      assert_operator path[:low], :<, path[:high], "#{path[:key]} is not a range"
      assert_operator path[:high], :<=, PagesHelper::LATENCY_SCALE_MAX,
                      "#{path[:key]} runs past the end of the scale"
    end
  end

  # Every figure on that page is a user report from a private Telegram group.
  # The `t.me/c/...` links resolve only for members of those groups, so they
  # would 404 for a visitor, and the reporters have not been asked whether they
  # want their names on the marketing site.
  test 'the low-latency page cites no private Telegram links and no unpublished meter' do
    %w[en ru zh].each do |locale|
      get "/low-latency?locale=#{locale}"

      assert_response :success
      assert_not_includes response.body, 't.me/c/', "a private Telegram link leaked into #{locale}"
      assert_includes response.body, ERB::Util.html_escape(I18n.t('pages.low_latency.latency_title', locale: locale)),
                      "the latency section is missing in #{locale}"
    end

    get '/low-latency?locale=en'
    assert_not_includes response.body, 'latency meter',
                        'the page claims a meter whose design and runs are not published'
  end
end
