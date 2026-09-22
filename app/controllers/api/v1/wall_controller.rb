# frozen_string_literal: true

# The five newest cameras on the Open Wall, as JSON (#160).
#
# The home page's mosaic used to be rendered by Rails, reading
# `Snapshot.latest_per_camera(limit: 5)` on every request. The page is
# prerendered now, so the tiles ship as the "no signal" placeholders the Rails
# template itself falls back to and this fills them in on load.
#
# Under /api/ because that is the one prefix #142 has the mirrors proxy to the
# origin uncached, and because deploy/static/reserved-paths reserves it -- so a
# file in the bundle can never shadow it.
#
# It answers a URL and not an image. The bytes are already on disk under
# /wall/, where nginx serves them without waking Ruby: that is what #166's
# predecessor was for, after the ActiveStorage redirect path turned into 62% of
# everything the site served.
module Api
  module V1
    class WallController < ApplicationController
      # How many tiles the mosaic holds. The page pads the rest with
      # placeholders, exactly as it does when the query returns nothing.
      TILES = 5

      # A minute. The cameras upload on a quarter-hour cron, so nothing is
      # lost by it, and the home page is the most-requested address on the
      # site -- every visitor would otherwise run the greatest-n-per-group
      # join that Snapshot#latest_per_camera's own comment measures at ~280ms.
      CACHE_SECONDS = 60

      def latest
        tiles = Snapshot.latest_per_camera(limit: TILES).filter_map { |snapshot| tile_for(snapshot) }

        expires_in CACHE_SECONDS.seconds, public: true
        render json: { snapshots: tiles }
      end

      private

      # nil for a snapshot whose variants have not been written yet. The page
      # shows a placeholder in its place, which is what it shows for a camera
      # that has not uploaded at all -- rather than a broken image, or a
      # redirect through ActiveStorage that nothing can cache.
      def tile_for(snapshot)
        return nil unless snapshot.variants_generated_at?

        {
          href: snapshot_path(id: snapshot),
          src: snapshot.wall_image(:thumb),
          caption: caption_for(snapshot),
        }
      end

      # soc and sensor are whatever the camera chose to send: both are
      # nullable, and the upload endpoint permits either to be absent. One such
      # upload used to take the whole homepage down with it.
      def caption_for(snapshot)
        [snapshot.soc, snapshot.sensor].reject(&:blank?).map(&:upcase).join(' · ')
      end
    end
  end
end
