# frozen_string_literal: true

require 'test_helper'

# The pages that predate the relaunch and survived it.
#
# They are the ones nobody thinks about, and they render through the layout,
# navbar and footer that the relaunch rewrote -- so a mistake in any of those
# shows up here first, or not at all. Six of these were served by the app and
# rendered by no test at all before this file.
class LegacyPagesTest < ActionDispatch::IntegrationTest
  # path => a string the page must contain, so the test fails on an empty or
  # wrong render rather than merely on a non-200.
  PAGES = {
    '/green_life' => 'green_life',
    '/merchandise' => 'merchandise',
    '/our-team' => 'our_team',
    '/stages-of-firmware-development' => 'stages_of_firmware_development',
    '/utilities' => 'utilities',
    '/web-interface' => 'web_interface',
    '/majestic-endpoints' => 'majestic_endpoints',
    '/tools/firmware-partitions-calculation' => 'firmware_partitions_calculation',
    '/tools/high-resolution-timer' => 'high_resolution_timer',
    '/tools/qr-code-generator' => 'qr_code_generator'
  }.freeze

  PAGES.each do |path, key|
    test "#{path} renders" do
      get path

      assert_response :success
      assert_includes response.body, I18n.t("pages.#{key}.title"),
                      "#{path} rendered without its own title"
      assert_no_match(/translation missing/i, response.body)
    end
  end

  # These keep the layout's wrapper, unlike the relaunched pages. Asserting it
  # here pins the other side of the content_for(:fullwidth) switch against the
  # pages that actually depend on it.
  test 'they render inside the layout wrapper' do
    PAGES.each_key do |path|
      get path

      assert_not_empty css_select('main > div.container.mb-4'), "#{path} lost the wrapper"
    end
  end

  # Every one of them renders the rewritten navbar and footer.
  test 'they carry the navigation and the footer' do
    PAGES.each_key do |path|
      get path

      assert_not_empty css_select('nav.navbar a.navbar-brand'), "#{path} has no navbar"
      assert_not_empty css_select('footer.site-footer'), "#{path} has no footer"
    end
  end

  # The page advertised a single T-shirt three times: the same image, the same
  # .shirt_1 keys and the same link, pasted into three cards.
  test 'merchandise lists each product once' do
    get '/merchandise'

    assert_equal 1, response.body.scan('weededwords.com').size
  end

  # The wall says what it is. It is linked from the navigation and the footer
  # now, so it gets visitors who cannot otherwise tell whether the images were
  # collected or volunteered.
  test 'the Open Wall explains itself' do
    get '/open-wall'

    assert_response :success
    assert_includes response.body, I18n.t('snapshots.index.intro_html')
  end
end
