# frozen_string_literal: true

require 'test_helper'

# Open Collective cannot be paid with a card issued in Russia. The old site's
# Russian section sent those visitors to PayWall instead; the August 2026
# relaunch dropped the fork, so every locale offered one button and the Russian
# audience -- the most active on the language switcher -- was shown one it could
# not press. The site had sent PayWall 9, 3, 11 and 7 subscribers a year from
# 2022 to 2025, and none afterwards, because the link no longer existed (#201).
#
# This is the regression test. It is not about which service is better; it is
# about never showing someone a payment button their card cannot use.
class DonationPathTest < ActionDispatch::IntegrationTest
  PAYWALL = 'paywall.pw/openipc'
  OPEN_COLLECTIVE = 'opencollective.com/openipc'

  # Scoped to <article>, the page's own content. The footer carries an Open
  # Collective link on every page of the site, so an unscoped count is 2
  # everywhere and says nothing about what this page offers.
  def article
    css_select('article').first.to_s
  end

  test 'the Russian donate page leads with PayWall' do
    get '/ru/donate'

    assert_response :success
    assert_select 'article a[href*=?]', PAYWALL, 1,
                  'the Russian page offers no card route that works from Russia'
  end

  # Underneath, not instead of: a Russian speaker with a card issued elsewhere
  # should still reach the public-accounting route.
  test 'the Russian page still offers Open Collective' do
    get '/ru/donate'

    assert_select 'article a[href*=?]', OPEN_COLLECTIVE, 1
  end

  test 'PayWall comes before Open Collective on the Russian page' do
    get '/ru/donate'

    assert_operator article.index(PAYWALL), :<, article.index(OPEN_COLLECTIVE),
                    'the route that works from Russia is not the first one offered'
  end

  # The other half of the fork, and the half that is easy to break by accident:
  # PayWall is a Russian card-subscription service and has no business on the
  # English or Chinese page.
  test 'the English and Chinese pages are unchanged' do
    ['/donate', '/zh/donate'].each do |path|
      get path

      assert_response :success
      assert_select 'article a[href*=?]', OPEN_COLLECTIVE, 1, "#{path} lost Open Collective"
      assert_select 'a[href*=?]', PAYWALL, false, "#{path} offers a Russia-only payment route"
    end
  end

  # The copy is Russia-only by design, so i18n-tasks is told not to expect it
  # elsewhere. That makes a typo in a key silent -- it would render as
  # "translation missing" in front of the one audience this page exists for.
  test 'the Russian copy actually renders' do
    get '/ru/donate'

    assert_no_match(/translation missing/i, response.body)
    assert_select 'article a[href*=?]', PAYWALL do |links|
      assert_no_match(/translation/i, links.first.text)
      assert links.first.text.present?, 'the PayWall button has no label'
    end
  end
end
