# frozen_string_literal: true

require 'json'

# How many people pay for OpenIPC every month, as the donate page says it (#198).
#
# Read from a file an hourly cron writes, never from the network. The Open
# Collective API is a third party on the request path otherwise: a slow answer
# would be a slow page, and an outage there would be an outage here -- on the
# one page whose whole job is to be asked for money. `deploy/oc-stats.sh` does
# the fetching and renames the result into place, so a reader sees a whole file
# or the previous one, never half of either.
#
# Modelled on ReleaseIndex, which solves the same shape of problem for the
# firmware index, down to re-reading only when the file on disk has moved on.
class SupportStats
  # What the page may say, and how long it may say it for.
  #
  # Refused rather than warned about, which is the opposite of ReleaseIndex.
  # A stale firmware index still answers every download it lists; a stale
  # backer count is a false claim about people, printed next to a request for
  # money. After two days the page renders exactly as it did before this
  # existed, which is the safe direction and the one #198 asks for.
  STALE_AFTER = 48.hours

  # How long a rendered page may still be handed out after it was built.
  #
  # The pages carrying this count are publicly cacheable, and the wizard's
  # policy is the longest on the site: one hour fresh plus a day of
  # stale-while-revalidate. So a response built a minute before the data went
  # stale could be served for another twenty-five hours, and the 48-hour rule
  # would be a rule about rendering rather than about what a reader sees.
  #
  # `freshness_window` caps the response instead, so no page outlives the
  # number printed on it. Taken from the longest FRESHNESS entry rather than
  # guessed, and asserted against it in the test.
  MAX_PAGE_LIFETIME = 3600 + 86_400

  attr_reader :backers, :monthly_cents, :fetched_at

  class << self
    def current
      path = stats_path
      return nil unless File.exist?(path)

      stamp = [File.mtime(path).to_f, File.size(path)]
      @current = nil if @stamp != stamp
      @stamp = stamp
      @current ||= parse(JSON.parse(File.read(path)))

      # Checked again on the way out, not only when the file changed.
      #
      # A file whose mtime never moves is exactly what a stopped cron looks
      # like -- and what a fetch that keeps failing looks like too, since
      # oc-stats.sh deliberately leaves the previous file alone. Without this,
      # the first request after a deploy memoises a fresh object and a
      # long-lived Puma worker keeps handing out that same count days later,
      # which is the one thing the 48-hour rule exists to prevent.
      @current if @current&.usable?
    rescue JSON::ParserError, SystemCallError, IOError => e
      # A file being renamed over can vanish between the exist? and the read.
      Rails.logger.warn "support stats: #{path} is unreadable: #{e.class}: #{e.message}"
      nil
    end

    # Test seam and a way for a deploy to prove the file is being read.
    def reset!
      @current = nil
      @stamp = nil
    end

    def goal
      @goal ||= YAML.load_file(Rails.root.join('config/support_goal.yml'))
                    .fetch('monthly_backers')
    end

    private

    # `null`, `[1,2]` and `42` are all valid JSON and none of them are this
    # file. Indexing them raises NoMethodError or TypeError, which is not among
    # the rescues above, so a one-character corruption became a 500 on the
    # donate page, the home page and the wizard at once.
    def parse(raw)
      return nil unless raw.is_a?(Hash)

      stats = new(backers: raw['backers'], monthly_cents: raw['monthly_cents'],
                  fetched_at: raw['fetched_at'])
      stats.usable? ? stats : nil
    end

    def stats_path
      ENV.fetch('SUPPORT_STATS_PATH', '/rails/shared/support-stats.json')
    end
  end

  def initialize(backers:, monthly_cents:, fetched_at:)
    @backers = backers
    @monthly_cents = monthly_cents
    @fetched_at = parse_time(fetched_at)
  end

  # A timestamp the cron did not write -- a hand-edited file, a shape change
  # upstream -- is a missing timestamp, not an exception. usable? then refuses
  # it like any other.
  def parse_time(value)
    return nil if value.blank?

    Time.zone.parse(value.to_s)
  rescue ArgumentError, TypeError
    nil
  end

  # Every way the file can be wrong, in one place. A count of zero is refused
  # too: it is what a failed fetch that still wrote a file would look like, and
  # "0 people support OpenIPC" is not a sentence this site should print.
  def usable?
    backers.is_a?(Integer) && backers.positive? &&
      monthly_cents.is_a?(Integer) && monthly_cents >= 0 &&
      fetched_at.present? && fetched_at > STALE_AFTER.ago
  end

  # Rounded, not floored. The view prints whole dollars, and integer division
  # turned 199 cents into $1 -- understating what people give, on the page
  # thanking them for it. Every amount Open Collective reports today is whole
  # dollars, which is exactly why this would have gone unnoticed.
  def monthly_usd
    (monthly_cents / 100.0).round
  end

  # Capped at the goal so the meter cannot overflow its track. Passing the goal
  # is a good problem and shows as a full bar until someone raises it in
  # config/support_goal.yml -- which is a pull request, deliberately, so the
  # number does not move on its own.
  def progress_percent
    return 100 if backers >= self.class.goal

    ((backers.to_f / self.class.goal) * 100).round
  end

  # What the page may claim for itself, given how old this number already is.
  #
  # nil when the remaining life is long enough that the controller's own policy
  # cannot outlive the data, which is the ordinary case: the cron runs hourly,
  # so this is normally 47 hours of headroom against 25 of exposure.
  def freshness_window
    remaining = (fetched_at + STALE_AFTER) - Time.current
    return nil if remaining > MAX_PAGE_LIFETIME

    [remaining.to_i, 0].max
  end
end
