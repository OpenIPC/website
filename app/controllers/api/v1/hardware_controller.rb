# frozen_string_literal: true

module Api
  module V1
    # What a visitor can do with each SoC, as a file the prerendered hardware
    # pages can read (#162).
    #
    # The catalogue itself is git-versioned YAML and the pages are built from it
    # (#161), but one column of those pages is not in the catalogue and cannot
    # be: `Soc#availability` is a question about what upstream has published,
    # answered from ReleaseIndex, which a cron refreshes from GitHub every hour.
    # A page built on Tuesday would tell a visitor on Friday that a chip has no
    # firmware when it got some on Wednesday -- and "no solution yet" is exactly
    # the sentence that makes somebody close the tab.
    #
    # So the page bakes what was true at build time and refreshes it from here
    # on load. Small enough to be cheap: 126 short strings, about 4 KB.
    #
    # No locale in it. These are states, not sentences; the page has the words
    # for each state in all three languages already.
    class HardwareController < ApplicationController
      # An endpoint, not a page: the layout, the locale negotiation and the
      # flash have nothing to do with it.
      skip_forgery_protection

      def availability
        socs = Soc.includes(:vendor).order(:urlname)

        render json: {
          generated_at: Time.current.iso8601,
          socs: socs.to_h { |soc| [soc.urlname, soc.availability.to_s] },
        }
      end
    end
  end
end
