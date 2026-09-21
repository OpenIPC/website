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
  def with_stats(backers: 27, monthly_cents: 53_500, fetched_at: Time.current, raw: nil)
    Dir.mktmpdir do |dir|
      path = File.join(dir, 'support-stats.json')
      File.write(path, raw || JSON.generate(
        'backers' => backers, 'monthly_cents' => monthly_cents,
        'monthly_counted' => 24, 'fetched_at' => fetched_at&.utc&.iso8601
      ))
      with_path(path) { yield }
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
end
