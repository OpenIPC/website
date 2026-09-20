# frozen_string_literal: true

# sitemap.xml, with an hreflang alternate per locale (#154).
#
# There was none: requesting /sitemap.xml fell through to the `*unmatched`
# catch-all and 302'd to the homepage, so every search engine that asked was
# told the site had no sitemap and then handed a page. Two thirds of the
# content is Russian and Chinese and none of it was discoverable.
#
# Generated rather than committed because the SoC catalogue is 126 rows in a
# database and a static file would be wrong the day a vendor is added.
class SitemapsController < ApplicationController
  # The pages a visitor can reach and a search engine should index. Deliberately
  # a list rather than a walk over Rails.application.routes: the routes include
  # the admin area, the API, thirty-odd redirects and two 410s, and a sitemap
  # that offers any of those is worse than no sitemap.
  MARKETING = %w[
    / /get-started /low-latency /ecosystem /business /community /donate
    /majestic-endpoints /green_life /our-team /stages-of-firmware-development
    /utilities /web-interface /supported-hardware/featured
    /supported-hardware/full-list /tools/firmware-partitions-calculation
    /tools/high-resolution-timer /tools/qr-code-generator
  ].freeze

  def show
    @entries = MARKETING.map { |path| entry(path) } + catalogue_entries
    render formats: :xml
  end

  private

  # One URL per locale, each carrying the full alternate set including itself.
  def entry(path)
    { alternates: locale_alternates(path) }
  end

  # The hardware catalogue, which is the part of the site with genuine long-tail
  # search value: someone who owns an SSC338Q searches for it by name.
  def catalogue_entries
    Soc.includes(:vendor).filter_map do |soc|
      next if soc.vendor.nil?

      entry("/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}")
    end
  end
end
