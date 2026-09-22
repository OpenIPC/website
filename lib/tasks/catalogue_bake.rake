# frozen_string_literal: true

# Does the home page still state the right number of SoCs? (#160)
#
# frontend/apps/site/src/data/catalogue.ts bakes `Soc.count` and the soc-vendor
# names, because a prerendered page cannot read a database. Baked numbers go
# stale silently, and this is the thing that makes that noisy -- but it cannot
# be a test: the figures come from the production catalogue and CI's database
# is empty, so a test would compare 126 against 0 and fail on every run.
#
# So it is a task, run where the data actually is:
#
#   docker exec openipc-web-prod bin/rails catalogue:check
#
# It exits non-zero on drift, which is what lets a cron or a deploy step notice.
# #161 moves the catalogue into git-versioned YAML and ends the need for it:
# the numbers become a function of the same tree the build already reads.
namespace :catalogue do
  BAKED = 'frontend/apps/site/src/data/catalogue.ts'

  desc 'Compare the home page\'s baked SoC figures against the database'
  task check: :environment do
    source = File.read(Rails.root.join(BAKED))

    baked_count = source[/export const SOC_COUNT = (\d+);/, 1]&.to_i
    baked_names = source[/SOC_VENDOR_NAMES = \[(.*?)\]/m, 1].to_s.scan(/'([^']+)'/).flatten

    live_count = Soc.count
    live_names = Vendor.soc_vendors.order(:name).pluck(:name)

    drift = []
    drift << "SOC_COUNT is #{baked_count}, the database says #{live_count}" if baked_count != live_count
    if baked_names != live_names
      drift << "SOC_VENDOR_NAMES is #{baked_names.inspect}, the database says #{live_names.inspect}"
    end

    if drift.empty?
      puts "#{BAKED} matches the database: #{live_count} SoCs, #{live_names.size} vendors"
      next
    end

    warn "#{BAKED} is stale:"
    drift.each { |line| warn "  #{line}" }
    warn 'Edit the file, rebuild the bundle, and install it. See the comment in it.'
    exit 1
  end
end
