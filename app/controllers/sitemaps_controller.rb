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
  #
  # The hardware catalogue is included as of #154, which localized its routes.
  # It is the part of the site with real long-tail search value, and it was
  # absent only because every catalogue route carries a vendor and a SoC that
  # `scope '(:locale)'` would have bound to :locale -- listing /ru/cameras/...
  # before that was fixed would have advertised URLs that redirect to the
  # English homepage, which is worse than listing none.
  #
  # Read from the database rather than listed, because it is 126 rows that
  # change when a vendor is added. CATALOGUE is appended to PAGES at request
  # time; sitemap_test asserts every URL advertised here actually renders, so
  # a vendor whose page 500s cannot reach a search engine through this file.
  PAGES = %w[
    / /get-started /low-latency /ecosystem /business /community /donate
    /majestic-endpoints /green_life /our-team /stages-of-firmware-development
    /utilities /web-interface /supported-hardware/featured
    /supported-hardware/full-list /tools/firmware-partitions-calculation
    /tools/high-resolution-timer /tools/qr-code-generator /open-wall
  ].freeze

  def show
    @entries = (PAGES + catalogue_paths).map { |path| entry(path) }
    render formats: :xml
  end

  private

  # Vendor pages and SoC pages, in the shape the catalogue itself links to.
  # /cameras/socs is deliberately absent: without ?vendor= it redirects to the
  # featured page, which is already listed.
  def catalogue_paths
    socs = Soc.includes(:vendor).order(:id)

    socs.map(&:vendor).uniq.map { |vendor| "/cameras/vendors/#{vendor.to_param}" } +
      socs.map { |soc| "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}" }
  end

  # One URL per locale, each carrying the full alternate set including itself.
  def entry(path)
    { alternates: locale_alternates(path) }
  end
end
