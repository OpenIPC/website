# frozen_string_literal: true

require 'test_helper'

# A page whose language depends on a request header has to say so, or a shared
# cache in front of it will serve the first visitor's language to everybody
# else. Nothing caches these responses today -- Cache-Control is still
# `max-age=0, private, must-revalidate` -- so this test is guarding a promise
# that only comes due in #155 (real cache headers) and Phase 7 (proxy_cache on
# the mirrors). By then a wrong Vary is invisible until somebody in Russia is
# handed a Chinese page, which is exactly the kind of bug that does not show up
# in a staging environment with one visitor.
class LocaleVaryTest < ActionDispatch::IntegrationTest
  def vary_tokens
    response.headers['Vary'].to_s.split(',').map(&:strip).reject(&:empty?)
  end

  test 'an unprefixed page declares that it varies by Accept-Language' do
    get '/', headers: { 'Accept-Language' => 'ru-RU,ru;q=0.9' }

    assert_response :success
    assert_includes vary_tokens, 'Accept-Language',
                    'the language of / is chosen from this header, so the header belongs in Vary'
  end

  test 'a prefixed page does not, because its language is in the address' do
    get '/ru'

    assert_response :success
    assert_not_includes vary_tokens, 'Accept-Language', <<~MESSAGE.chomp
      /ru is Russian for everyone who opens it. Claiming it varies by
      Accept-Language would split the cache entry per browser language and
      throw away the one property that makes a prefixed URL cacheable.
    MESSAGE
  end

  # The bug this shape invites is assignment instead of append: Rails sets
  # `Vary: Accept` from format negotiation, and overwriting it would quietly
  # make every JSON and HTML response share one cache entry. Rather than
  # hardcode what Rails happens to set, take the prefixed response -- which
  # goes through the same rendering and skips our addition -- as the baseline.
  test 'it adds to Vary rather than replacing what Rails put there' do
    get '/ru'
    baseline = vary_tokens

    get '/'
    negotiated = vary_tokens

    baseline.each do |token|
      assert_includes negotiated, token,
                      "#{token.inspect} was dropped from Vary; the header was assigned, not appended"
    end
    assert_equal negotiated.uniq, negotiated, 'Vary lists a value twice'
  end
end
