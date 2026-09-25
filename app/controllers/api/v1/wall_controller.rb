# frozen_string_literal: true

module Api
  module V1
    # The tiles a prerendered page draws, and the permission to ask for them.
    #
    # `/` is rendered by Rails and its mosaic comes out of WallHelper: canvases
    # carrying an id and a variant, and a signed WallGrant naming exactly those
    # pairs. `/ru` and `/zh` are prerendered, so nothing renders them a grant --
    # and their mosaic showed five "no signal" tiles while the same page in the
    # same language showed live cameras at `/`. A visitor reached the broken one
    # by clicking their own language.
    #
    # This is the address that fixes that. It hands out no image bytes and no
    # image URL: the frames still arrive over WallChannel, still masked, still
    # inside the channel's per-address budget. What it adds is the one thing a
    # page that Rails did not render cannot have -- the grant.
    #
    # It discloses nothing `/open-wall` does not. That page is public, is
    # microcached, and carries a grant naming eighteen `thumb` pairs in its
    # HTML; this names at most twenty-four, of the same variant, of the same
    # snapshots. `fullhd` is deliberately not reachable here: the snapshot page
    # grants it for the one frame it has just drawn, and that is the only place
    # it should ever be granted from.
    class WallController < ApplicationController
      # An endpoint, not a page: the layout, the locale negotiation and the
      # flash have nothing to do with it.
      skip_forgery_protection

      # The mosaic is five tiles, and the number is the server's.
      #
      # It was `?limit=`, and that was a bug: both vhosts cache this on $uri,
      # which drops the query, so whichever count filled the cache first was
      # the one every home page got for the next minute -- and the grant with
      # it. A parameter that changes the body has to be in the cache key or not
      # exist, and this one has no caller that needs it. Found by review on
      # #277; if #165 wants another count it can add one with the key to match.
      TILES = 5

      # The one variant this address may authorise. A grant names id:variant
      # PAIRS for the reason WallGrant spells out -- separate id and variant
      # sets let a client pair a full-resolution permission with an id it had
      # only ever been shown as a thumbnail.
      VARIANT = 'thumb'

      def mosaic
        tiles = Snapshot.latest_per_camera(limit: TILES)
        grant = WallGrant.issue(pairs: tiles.map { |s| WallGrant.pair(s.public_id, VARIANT) })

        # A minute, and nothing in the body that changes between renders.
        #
        # No `generated_at`: WallGrant buckets its expiry to the cache window so
        # that the same ids sign to the same bytes, which is what lets a wall
        # page be microcached at all. A timestamp per render would undo that
        # here for no reader's benefit -- Cache-Control already says how fresh
        # this is.
        expires_in 60.seconds, public: true

        # Readable from a mirror. openipc.kz and openipc.cloud serve this
        # bundle from their own hosts, and a versioned public API that only
        # the canonical origin's own pages may read is one the mirrors cannot
        # use -- the same reason the backer count is CORS-open. There is
        # nothing here that is not already in /open-wall's HTML.
        response.set_header('Access-Control-Allow-Origin', '*')

        render json: { variant: VARIANT, grant: grant, tiles: tiles.map { |s| tile(s) } }
      end

      private

      # soc and sensor are whatever the camera chose to send: both are nullable,
      # and the upload endpoint permits either to be absent. One such upload
      # used to take the whole home page down with it.
      def tile(snapshot)
        { id: snapshot.public_id, soc: snapshot.soc.presence, sensor: snapshot.sensor.presence }
      end

    end
  end
end
