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
    / /get-started /low-latency /teleoperation /ecosystem /business /community /donate
    /majestic-endpoints /green_life /our-team /stages-of-firmware-development
    /utilities /web-interface /supported-hardware/featured
    /supported-hardware/full-list /tools/firmware-partitions-calculation
    /tools/high-resolution-timer /tools/qr-code-generator /open-wall
    /privacy
  ].freeze

  def show
    @entries = (PAGES + catalogue_paths).map { |path| entry(path) }
    render formats: :xml
  end

  private

  # Vendor pages and SoC pages, in the shape the catalogue itself links to.
  # /cameras/socs is deliberately absent: without ?vendor= it redirects to the
  # featured page, which is already listed.
  #
  # Built with the route helpers rather than by interpolating slugs into a
  # string. `urlname` is a free text column an admin can edit, and a value
  # holding a "/", a "?" or a "#" interpolated raw would split one entry into
  # extra path segments, a query or a fragment -- advertising URLs the
  # catalogue routes, which take one segment per identifier, cannot serve. The
  # helpers escape it, and `locale: nil` keeps these as the English forms that
  # locale_alternates then derives the other two from.
  #
  # A SoC whose vendor row has gone is skipped rather than raised on. The
  # association is required at the model, so this should not happen -- but the
  # database has no foreign key to enforce it, and the cost of being wrong is
  # the entire sitemap answering 500 instead of one chip being absent from it.
  def catalogue_paths
    socs = Soc.includes(:vendor).order(:id).select { |soc| soc.vendor.present? }

    socs.map(&:vendor).uniq.map { |vendor| cameras_vendor_path(id: vendor, locale: nil) } +
      socs.map { |soc| cameras_vendor_soc_path(vendor_id: soc.vendor, id: soc, locale: nil) }
  end

  # One URL per locale, each carrying the full alternate set including itself.
  def entry(path)
    { alternates: locale_alternates(path) }
  end
end
