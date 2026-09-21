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

  attr_reader :backers, :monthly_cents, :fetched_at

  class << self
    def current
      path = stats_path
      return nil unless File.exist?(path)

      stamp = [File.mtime(path).to_f, File.size(path)]
      @current = nil if @stamp != stamp
      @stamp = stamp
      @current ||= parse(JSON.parse(File.read(path)))
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

    def parse(raw)
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
    @fetched_at = fetched_at.present? ? (Time.zone.parse(fetched_at.to_s) rescue nil) : nil
  end

  # Every way the file can be wrong, in one place. A count of zero is refused
  # too: it is what a failed fetch that still wrote a file would look like, and
  # "0 people support OpenIPC" is not a sentence this site should print.
  def usable?
    backers.is_a?(Integer) && backers.positive? &&
      monthly_cents.is_a?(Integer) && monthly_cents >= 0 &&
      fetched_at.present? && fetched_at > STALE_AFTER.ago
  end

  def monthly_usd
    monthly_cents / 100
  end

  # Capped at the goal so the meter cannot overflow its track. Passing the goal
  # is a good problem and shows as a full bar until someone raises it in
  # config/support_goal.yml -- which is a pull request, deliberately, so the
  # number does not move on its own.
  def progress_percent
    return 100 if backers >= self.class.goal

    ((backers.to_f / self.class.goal) * 100).round
  end
end
