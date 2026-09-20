# frozen_string_literal: true

require 'test_helper'

# The beacon must stay first-party. Everything it measures -- language,
# country, referrer, whether an address is a person or a proxy-checker -- is
# the sort of data that vendors are glad to host for free, and the decision
# taken in #181 was that none of it leaves this host: no third party, no
# cookie, no localStorage.
#
# That decision lives in two characters of a src attribute, so this is what
# stops a later "let us just use Plausible's CDN" edit from passing review on
# the grounds that the dashboard still works.
class AnalyticsBeaconTest < ActionDispatch::IntegrationTest
  setup { get '/' }

  def beacon
    css_select('script[src*="/api/a/"]').first
  end

  test 'every page carries the beacon' do
    assert_not_nil beacon, 'the counting script is not in the layout'
  end

  test 'it is same-origin, with no host of its own' do
    src = beacon['src']

    assert src.start_with?('/'), <<~MESSAGE.chomp
      The beacon is loaded from #{src.inspect}.

      It must be a root-relative path. A scheme or a host here sends every
      visitor's address, page and referrer to somebody else, which is the one
      thing #181 decided against.
    MESSAGE
    assert_equal '/api/a/count', beacon['data-goatcounter'],
                 'the endpoint the beacon reports to must be same-origin too'
  end

  # Both tags are deferred and count.js must come second: src/analytics.js sets
  # window.goatcounter.no_onload, and count.js reads it when it runs. Deferred
  # scripts run in document order, so the order in the document IS the
  # guarantee. Reversed, or made async, count.js wins the race, never sees the
  # config, and counts the load event -- which under Turbo Drive fires once per
  # session, so a visitor who reads twenty pages registers one.
  test 'it is deferred and runs after the bundle that configures it' do
    assert beacon.attributes.key?('defer'), 'the beacon must be deferred'
    assert_not beacon.attributes.key?('async'),
               'async makes the order below a race rather than a guarantee'

    scripts = css_select('script[src]').map { |tag| tag['src'] }
    bundle = scripts.index { |src| src.include?('application') }
    counter = scripts.index { |src| src.include?('/api/a/') }

    assert_not_nil bundle, 'the application bundle is not in the layout'
    assert_operator bundle, :<, counter,
                    'count.js runs before the script that sets no_onload, so it counts the load event'
  end

  test 'counting sets no cookie' do
    assert_nil response.headers['Set-Cookie']&.match(/goatcounter|_ga|analytics/i),
               'the counter must stay cookieless'
  end
end
