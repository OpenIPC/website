# frozen_string_literal: true

require 'test_helper'

# The backer count the donate page prints (#198).
#
# Individual monthly backers went 43 -> 38 -> 28 over 2024-2026 and the page
# showed no number at all. A visible "27 people keep this running, help us
# reach 50" turns an abstract ask into a countable one.
#
# What these hold is the half that can go wrong silently. The number comes from
# a file a cron writes, and every way that file can be absent, stale or wrong
# has to end with the page rendering exactly as it did before the count
# existed -- never a stale number, and never a zero, beside a request for money.
class SupportCountTest < ActionDispatch::IntegrationTest
  DEFAULT_STATS = { 'backers' => 27, 'monthly_cents' => 53_500, 'monthly_counted' => 24 }.freeze

  # Overrides by key rather than by keyword, so adding a field to the file does
  # not lengthen this signature every time.
  def with_stats(raw: nil, fetched_at: Time.current, **overrides, &block)
    Dir.mktmpdir do |dir|
      path = File.join(dir, 'support-stats.json')
      body = DEFAULT_STATS
             .merge('fetched_at' => fetched_at&.utc&.iso8601)
             .merge(overrides.transform_keys(&:to_s))
             .compact
      File.write(path, raw || JSON.generate(body))
      with_path(path, &block)
    end
  end

  def with_path(path)
    ENV['SUPPORT_STATS_PATH'] = path
    SupportStats.reset!
    yield
  ensure
    ENV.delete('SUPPORT_STATS_PATH')
    SupportStats.reset!
  end

  # --- the three states #198 names ---

  test 'with a fresh file the page shows the count, the goal and the meter' do
    with_stats do
      get '/donate'

      assert_response :success
      assert_select '.support-count', 1
      assert_match(/27 people/, response.body)
      assert_select '.support-meter[aria-valuenow=?]', '27'
      assert_select '.support-meter[aria-valuemax=?]', SupportStats.goal.to_s
    end
  end

  test 'with no file at all the page renders as it did before' do
    with_path('/nonexistent/support-stats.json') do
      get '/donate'

      assert_response :success
      assert_select '.support-count', 0
      assert_select 'a[data-event="oc-checkout"]', 1, 'the donate page stopped working entirely'
    end
  end

  # The one that matters most. A number three days old is a false claim about
  # people, printed next to a request for money.
  test 'a stale file is refused, not printed' do
    with_stats(fetched_at: 3.days.ago) do
      get '/donate'

      assert_response :success
      assert_select '.support-count', 0
    end
  end

  # --- the ways a written file can still be wrong ---

  { 'a zero count' => { backers: 0 },
    'a negative count' => { backers: -3 },
    'a count that is not a number' => { backers: 'twenty-seven' },
    'no timestamp' => { fetched_at: nil } }.each do |name, attrs|
    test "#{name} is refused" do
      with_stats(**attrs) do
        get '/donate'

        assert_response :success
        assert_select '.support-count', 0
      end
    end
  end

  test 'a half-written file is refused rather than raised on' do
    with_stats(raw: '{"backers": 27, "monthly_') do
      get '/donate'

      assert_response :success
      assert_select '.support-count', 0
    end
  end

  # --- the three placements share one number ---

  test 'the same count appears on the home band and in the wizard' do
    with_stats do
      get '/'

      assert_response :success
      assert_select '.section--ink .support-count', 1, 'not under the home CTA band'

      vendor = Vendor.create!(name: 'Support Probe Vendor')
      soc = Soc.create!(vendor: vendor, model: 'SUPPROBE', status: 'done',
                        uboot_filename: 'u.bin', linux_filename: 'l.tgz')
      get "/cameras/vendors/#{vendor.to_param}/socs/#{soc.to_param}",
          params: { camera: { flash_type: 'nor8m', firmware_version: 'lite',
                              network_interface: 'eth', sd_card_slot: 'nosd' } }

      assert_response :success
      assert_select '.alert-success .support-count', 1, 'not in the wizard success block'
    end
  end

  test 'it is translated, not left in English' do
    with_stats do
      %w[/ru/donate /zh/donate].each do |path|
        get path

        assert_select '.support-count', 1
        assert_no_match(/translation missing/i, response.body)
        assert_not_includes response.body, 'support OpenIPC every month',
                            "#{path} shows the English sentence"
      end
    end
  end

  # --- the meter ---

  test 'the meter cannot overflow its track' do
    with_stats(backers: SupportStats.goal + 20) do
      get '/donate'

      assert_select '.support-meter span[style=?]', 'width: 100%'
    end
  end

  # `progress` announces a task completing. This is people against a target
  # somebody chose, which is what `meter` is for.
  test 'it is a meter, for a screen reader too' do
    with_stats do
      get '/donate'

      assert_select '[role="meter"][aria-label]', 1
      assert_select '.support-count progress', 0
    end
  end

  # --- the four things review found ---

  # A file whose mtime never moves is what a stopped cron looks like, and what
  # a fetch that keeps failing looks like too. Without re-checking on the way
  # out, the first request memoises a fresh object and a long-lived worker
  # hands out that same count days later.
  test 'a memoised count goes stale like any other' do
    Dir.mktmpdir do |dir|
      path = File.join(dir, 'support-stats.json')
      File.write(path, JSON.generate('backers' => 27, 'monthly_cents' => 53_500,
                                     'fetched_at' => Time.current.utc.iso8601))
      with_path(path) do
        assert SupportStats.current, 'not usable even when fresh'

        # Same file, same mtime, later clock -- exactly a cron that stopped.
        travel 3.days do
          assert_nil SupportStats.current, 'a memoised object outlived its window'
        end
      end
    end
  end

  # null, [1,2] and 42 are all valid JSON and none of them are this file.
  # Indexing them raises outside the rescued set, which turned a one-character
  # corruption into a 500 on three pages at once.
  ['null', '[1, 2]', '"hello"', '42'].each do |body|
    test "a JSON root of #{body} hides the count instead of raising" do
      with_stats(raw: body) do
        get '/donate'

        assert_response :success
        assert_select '.support-count', 0
      end
    end
  end

  test 'cents are rounded, not floored' do
    with_stats(monthly_cents: 199) do
      get '/donate'

      assert_select '.support-count', 1
      assert_match(/\$2\b/, response.body, 'understated what people give')
      assert_no_match(/\$1\b/, response.body)
    end
  end

  # --- the page must not outlive the number on it ---

  # The longest policy on the site is the catalogue's, and the wizard is in it.
  # If that grows, MAX_PAGE_LIFETIME has to grow with it or the cap stops
  # covering the worst case -- which is the kind of drift nothing would notice.
  test 'the cap covers the longest freshness policy the site declares' do
    longest = (ApplicationController::FRESHNESS.values + [ApplicationController::DEFAULT_FRESHNESS])
              .map { |f| f[:max_age] + f[:swr] }.max

    assert_operator SupportStats::MAX_PAGE_LIFETIME, :>=, longest,
                    'a page can be served for longer than the count it prints is allowed to live'
  end

  test 'a fresh count leaves the page cacheable as usual' do
    with_stats(fetched_at: 1.hour.ago) do
      get '/donate'

      assert_response :success
      # 47 hours of headroom against 25 of exposure: nothing to cap.
      assert_nil SupportStats.current.freshness_window
    end
  end

  # Asserted on the wizard, not /donate. /donate's policy is five minutes,
  # which is already inside any window this could cap, so the assertion passed
  # with the cap removed -- it proved nothing. The catalogue's policy is the
  # one that outlives the data: an hour fresh plus a day of stale serving.
  test 'a count near the end of its life shortens the page it is on' do
    vendor = Vendor.create!(name: 'Freshness Probe Vendor')
    soc = Soc.create!(vendor: vendor, model: 'FRESHPROBE', status: 'done',
                      uboot_filename: 'u.bin', linux_filename: 'l.tgz')

    with_stats(fetched_at: (SupportStats::STALE_AFTER - 2.hours).ago) do
      get "/cameras/vendors/#{vendor.to_param}/socs/#{soc.to_param}",
          params: { camera: { flash_type: 'nor8m', firmware_version: 'lite',
                              network_interface: 'eth', sd_card_slot: 'nosd' } }

      assert_response :success
      assert_select '.support-count', 1

      header = response.headers['Cache-Control'].to_s
      max_age = header[/max-age=(\d+)/, 1].to_i
      swr = header[/stale-while-revalidate=(\d+)/, 1].to_i

      assert_operator max_age + swr, :<=, 2.hours.to_i,
                      "the page may be served for #{max_age + swr}s after a number good for 7200s"
      assert_operator max_age, :>, 0, 'the page stopped being cacheable at all'
    end
  end

  # The ordinary case on that same page: nothing is capped, and the catalogue
  # keeps the long policy it is given.
  test 'a fresh count leaves the long catalogue policy alone' do
    vendor = Vendor.create!(name: 'Freshness Probe Vendor 2')
    soc = Soc.create!(vendor: vendor, model: 'FRESHPROBE2', status: 'done',
                      uboot_filename: 'u.bin', linux_filename: 'l.tgz')

    with_stats(fetched_at: 1.hour.ago) do
      get "/cameras/vendors/#{vendor.to_param}/socs/#{soc.to_param}",
          params: { camera: { flash_type: 'nor8m', firmware_version: 'lite',
                              network_interface: 'eth', sd_card_slot: 'nosd' } }

      assert_equal 3600, response.headers['Cache-Control'][/max-age=(\d+)/, 1].to_i
    end
  end

  # --- both channels (#201) ---

  # Open Collective cannot be paid with a card issued in Russia, so PayWall is
  # not a rounding error on the count -- it is where a whole audience gives.
  test 'the count is both channels, and says which is which' do
    with_stats(backers: 50, backers_oc: 27, paywall_subscribers: 23) do
      get '/donate'

      assert_response :success
      assert_match(/50 people/, response.body)
      assert_match(/27 through Open Collective and 23 through PayWall/, response.body)
    end
  end

  # The ordinary case on a host with no PayWall export: one number, no split,
  # and nothing claiming a channel that was not counted.
  test 'with no PayWall figures it says nothing about PayWall' do
    with_stats(backers: 27) do
      get '/donate'

      assert_select '.support-count', 1
      assert_no_match(/PayWall/i, css_select('.support-count').to_s)
    end
  end

  # Adding PayWall took the count from 27 to exactly the goal of 50 on the day
  # the two were first counted together. "Help us reach 50" beside a 50 is a
  # mistake the reader sees before we do.
  test 'a met goal is not still asking to be reached' do
    with_stats(backers: SupportStats.goal) do
      get '/donate'

      assert_select '.support-count', 1
      assert_match(/goal met/i, response.body)
      assert_no_match(/Help us reach/i, response.body)
    end
  end

  test 'an unmet goal still asks' do
    with_stats(backers: SupportStats.goal - 1) do
      get '/donate'

      assert_match(/Help us reach/i, response.body)
      assert_no_match(/goal met/i, response.body)
    end
  end
end
