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

      # The one variant the mosaic may authorise. A grant names id:variant
      # PAIRS for the reason WallGrant spells out -- separate id and variant
      # sets let a client pair a full-resolution permission with an id it had
      # only ever been shown as a thumbnail.
      VARIANT = 'thumb'

      # The wall's own numbers, and they are not this controller's to choose.
      # Each address below authorises exactly what the Rails page at the same
      # address authorises today -- same count, same variants -- so moving a
      # page to the bundle changes who renders it and nothing about what a
      # fetch of it is worth to a harvester. `wall_controller_test` asserts
      # both against SnapshotsController rather than trusting this comment.
      PER_PAGE = 18
      STRIP_EAGER = 12

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

      # /open-wall, as data: one page of the latest frame per camera.
      #
      # The page number is a path segment and not a query parameter, and that
      # is the same lesson `TILES` above records: both vhosts cache on $uri,
      # which drops the query, so `?page=2` would have served page two to
      # everyone who asked for page one for the next minute -- with a grant
      # naming page two's ids.
      def page
        number = [params[:page].to_i, 1].max
        all = Snapshot.latest_per_camera
        rows = all[((number - 1) * PER_PAGE), PER_PAGE] || []

        render_wall(pairs: pairs_for(rows, 'thumb'),
                    body: { variant: 'thumb', page: number, pages: page_count(all),
                            tiles: rows.map { |s| card(s) } })
      end

      # /snapshots/<id>, as data: the frame itself and the first screenful of
      # its camera's day.
      #
      # `STRIP_EAGER + 1` rows and no COUNT, exactly as the Rails page does it:
      # this is the most-fetched address on the site and the join behind it is
      # not free. The extra row is how `strip_more` is known.
      def snapshot
        subject = find_snapshot or return
        rows = day_of(subject).limit(STRIP_EAGER + 1).to_a
        strip = rows.first(STRIP_EAGER)

        render_wall(pairs: pairs_for(strip, 'icon2') + pairs_for([subject], 'fullhd'),
                    body: { variant: 'fullhd', strip_variant: 'icon2',
                            snapshot: detail(subject), strip: strip.map { |s| icon(s) },
                            strip_more: rows.size > STRIP_EAGER })
      end

      # /snapshots/<id>/archive, as data: every frame of the day as an icon,
      # newest first. Granted at icon2 and nothing larger, which is what the
      # archive page grants -- the day at full resolution is the slideshow's
      # handover and is made once, below, rather than twice.
      def archive
        subject = find_snapshot or return
        rows = day_of(subject).to_a

        render_wall(pairs: pairs_for(rows, 'icon2'),
                    body: { variant: 'icon2', frames: rows.map { |s| icon(s) } })
      end

      # /snapshots/<id>/oneday, as data: the same day oldest first, at full
      # resolution, because that is what a slide is.
      #
      # Truncating this was considered and rejected on 2026-09-24, and the
      # reasoning transfers unchanged: WallChannel::FRAME_BUDGET is the binding
      # constraint, so an address limited to N frames an hour takes N whether
      # it collects them 79 at a time or 24 -- capping here would cost a
      # guaranteed feature and buy a factor no harvester would feel.
      def slideshow
        subject = find_snapshot or return
        rows = day_of(subject).reverse_order.to_a

        render_wall(pairs: pairs_for(rows, 'fullhd'),
                    body: { variant: 'fullhd', frames: rows.map { |s| icon(s) } })
      end

      private

      def pairs_for(rows, variant)
        rows.map { |s| WallGrant.pair(s.public_id, variant) }
      end

      def page_count(rows)
        [(rows.size / PER_PAGE.to_f).ceil, 1].max
      end

      # One shape for every address here: a grant, a minute of freshness, and
      # readable from a mirror.
      def render_wall(pairs:, body:)
        expires_in 60.seconds, public: true
        response.set_header('Access-Control-Allow-Origin', '*')

        render json: body.merge(grant: WallGrant.issue(pairs: pairs))
      end

      # By public_id, never by row id, and a numeric id is gone rather than
      # missing -- the same answer SnapshotsController gives, so that moving
      # the page does not change what a stale link does.
      def find_snapshot
        if params[:id].to_s.match?(/\A[0-9]+\z/)
          head :gone
          return nil
        end

        Snapshot.find_by(public_id: params[:id]) || (head(:not_found) && nil)
      end

      # A camera's last day, newest first. The MAC never leaves the server: it
      # is the join key here and a camera_token everywhere a reader can see.
      def day_of(subject)
        Snapshot.where(mac_address: subject.mac_address,
                       created_at: [1.day.ago..Time.now]).order(created_at: :desc, id: :desc)
      end

      # What a strip icon and a slide need, and nothing else: an address and a
      # time. No soc, no firmware, no byte size -- a day of those is a
      # description of one camera's hardware and uptime, repeated 96 times.
      def icon(snapshot)
        { id: snapshot.public_id, at: snapshot.created_at.to_i }
      end

      # What the snapshot page prints above the fold.
      def detail(snapshot)
        card(snapshot).merge(
          caption: snapshot.caption.presence,
          camera: snapshot.camera_token
        )
      end

      # soc and sensor are whatever the camera chose to send: both are nullable,
      # and the upload endpoint permits either to be absent. One such upload
      # used to take the whole home page down with it.
      def tile(snapshot)
        { id: snapshot.public_id, soc: snapshot.soc.presence, sensor: snapshot.sensor.presence }
      end

      # What a card on /open-wall prints and the mosaic does not: the firmware
      # line, the uptime, the size. The blob is preloaded by latest_per_camera,
      # so byte_size costs no query there -- it would cost one per row anywhere
      # else, which is what that preload exists to stop.
      def card(snapshot)
        tile(snapshot).merge(
          firmware: snapshot.firmware.presence, streamer: snapshot.streamer.presence,
          uptime: snapshot.uptime.presence, soc_temperature: snapshot.soc_temperature.presence,
          dimensions: snapshot.image_dimensions, bytes: snapshot.file.byte_size,
          at: snapshot.created_at.to_i
        )
      end
    end
  end
end
