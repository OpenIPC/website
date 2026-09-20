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

  # count.js reads window.goatcounter when it runs, and what it finds decides
  # whether the site counts pages or sessions. `no_onload` off means it counts
  # the document load event, which under Turbo Drive fires once per SESSION --
  # so a visitor who reads twenty pages registers one, and the dashboard looks
  # plausible while being wrong by a factor of twenty.
  #
  # The configuration is therefore an INLINE script, which runs at parse time,
  # and count.js is deferred, which runs after parsing. That ordering holds
  # whatever the bundler does. It used to live in the bundle and rely on
  # deferred scripts executing in document order -- true, but it made the
  # guarantee depend on how application.js is built, which is not something
  # this file can see.
  test 'the beacon is deferred, never async' do
    assert beacon.attributes.key?('defer'), 'the beacon must be deferred'
    assert_not beacon.attributes.key?('async'),
               'async would let count.js run before the page is parsed'
  end

  test 'the configuration is set before count.js can read it' do
    body = response.body
    config = body.index('window.goatcounter')
    counter = body.index('/api/a/c.js')

    assert_not_nil config, 'nothing in the layout sets window.goatcounter'
    assert_operator config, :<, counter, <<~MESSAGE.chomp
      The script that configures the counter comes after count.js.

      count.js reads window.goatcounter when it runs. Without no_onload it
      counts the document load event, which Turbo fires once per session.
    MESSAGE
  end

  test 'the configuration script is not deferred, or the ordering means nothing' do
    config_tag = css_select('script:not([src])').find { |tag| tag.text.include?('window.goatcounter') }

    assert_not_nil config_tag, 'the configuration is not an inline script'
    assert_not config_tag.attributes.key?('defer'),
               'a deferred config would run after parsing, alongside count.js rather than before it'
    assert_includes config_tag.text, 'no_onload',
                    'without no_onload, count.js counts the load event and Turbo fires it once per session'
  end

  test 'counting sets no cookie' do
    assert_nil response.headers['Set-Cookie']&.match(/goatcounter|_ga|analytics/i),
               'the counter must stay cookieless'
  end
end
