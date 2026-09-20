# frozen_string_literal: true

require 'test_helper'

# The clicks that matter all leave the site, so the server never sees them
# (#183): the business page ends in a mailto:, the donate page in links to two
# payment services, the community page in Telegram invites. Two percent of
# visitors reach /business or /donate at all, and until now whether any of them
# went on to click was simply unknown.
#
# What makes that worth a test rather than a comment: a missing data-event is
# invisible. The page looks right, the link works, and the dashboard shows a
# zero that reads as "nobody clicked" rather than "nobody counted".
class ClickEventsTest < ActionDispatch::IntegrationTest
  # path => [event name, how many links should carry it]
  CONVERSIONS = {
    '/business' => ['business-mail', 1],
    '/community' => ['tg-join', 4],
    '/donate' => ['oc-checkout', 1],
    '/ru/donate' => ['paywall-checkout', 1]
  }.freeze

  test 'every conversion link names its event' do
    CONVERSIONS.each do |path, (event, count)|
      get path

      assert_response :success
      assert_select "a[data-event=?]", event, count,
                    "#{path} does not name #{event}, so its clicks arrive as nothing"
    end
  end

  # Both donation routes have to be measured in the same units or #201's
  # question -- does the Russian audience use the route that works for them --
  # cannot be answered. The Russian page's Open Collective link lives in a
  # locale string, which is exactly where a data-event gets forgotten.
  test 'both donation routes are counted, on the page that offers both' do
    get '/ru/donate'

    assert_select 'a[data-event=?]', 'paywall-checkout', 1
    assert_select 'a[data-event=?]', 'oc-checkout', 1,
                  'the fallback would be counted as a generic outbound click instead'
  end

  test 'the home band names its Telegram button' do
    get '/'

    assert_select 'a[data-event=?]', 'tg-join', 1
  end

  # The band is shared by five pages, most of whose buttons point at this site,
  # where a page view already records the visit. Naming those would count one
  # visit twice.
  test 'shared bands stay unnamed where the destination is this site' do
    get '/get-started'

    assert_select 'a[data-event][href^="/"]', false,
                  'an internal link is counted as an event as well as a page view'
  end
end
