# frozen_string_literal: true

# Permission to receive a particular set of frames, issued by the page that
# already showed them.
#
# WHY THIS EXISTS. #267 moved frames off static files and onto WallChannel, so
# that a crawler collecting URLs would find nothing to collect. That worked --
# no address returns a camera frame, and the day after the cutover the access
# log reported zero. What it did not anticipate is a client that implements our
# protocol instead of fetching our URLs. In the first four hours, 991 of the
# 1,055 addresses on the channel had requested NOTHING else: no page, no asset,
# no favicon, just `GET /api/v1/wall/cable`. They took 2,393 MiB of the 2,437.
#
# FRAME_BUDGET did not stop them because it is keyed per address and they used
# each address once, for about 96 frames -- the fifth per-address control on
# this surface to be beaten by distribution, after opaque ids (#235), the
# JavaScript gate (#261), the address block (#262) and a per-address rate
# limit. WallChannel's own header says the budget "is not what stops a
# distributed harvester". Nothing else was doing that job.
#
# The error was that the SOCKET was the session, and anybody may open a socket.
# A session has to begin where the server decides what a client is allowed to
# see, which is the page render. A grant is that decision, made portable.
#
# WHAT IT IS NOT. It does not make the wall unharvestable, and saying so would
# repeat the overstatement that #235 and #261 were sold on. Someone who fetches
# pages gets a grant with each one and may use it. What changes is that the
# harvest is then bounded by the page-fetch rate -- and page fetches are plain
# HTTP requests that nginx already governs, through limit_req on the snapshot
# pages, limit_conn, the datacentre block and the microcache. Today the socket
# bypasses every one of those. This puts the harvest back under the controls
# that already exist, where it can be measured and tuned.
class WallGrant
  # Long enough for the slowest legitimate page to finish asking.
  #
  # The slideshow is the bound: 96 frames delivered in chunks of 8 at 250 ms,
  # which is about three seconds of requesting, and a reader may leave it open
  # far longer while the carousel cycles every three seconds through a
  # four-minute loop. Ten minutes covers the whole loop several times over and
  # still makes a captured grant close to worthless -- the frames it names are
  # ones its bearer was already shown.
  TTL = 10.minutes

  # A grant may not name more frames than a page can show. The archive and the
  # slideshow are the widest at 96, which is MAX_PER_REQUEST for the same
  # reason: the longest day a camera can produce.
  MAX_IDS = 256

  # Renders inside one bucket produce a byte-identical grant, and that is a
  # requirement rather than an optimisation.
  #
  # Wall pages are microcached by nginx for 300 seconds on the key
  # `$scheme$host|$locale_key|$uri`, which contains no address and no nonce. So
  # ONE render is what every reader receives for the next five minutes. A grant
  # carrying a fresh timestamp per render would make each render a different
  # body, and `wall_supply_line_test.rb` already fails that -- it exists
  # because whichever body wins the cache is the one everybody gets.
  #
  # Bucketing the expiry to the cache window makes the page stable again: the
  # same ids in the same five minutes sign to the same bytes, because a
  # MessageVerifier is an HMAC and is deterministic over its payload.
  #
  # A grant therefore lives between TTL - CACHE_WINDOW and TTL: at least five
  # minutes, which comfortably outlives the cache entry that carries it.
  CACHE_WINDOW = 300

  class << self
    def issue(ids:, variants:)
      ids = Array(ids).uniq.sort.first(MAX_IDS)
      return nil if ids.empty?

      verifier.generate({ 'i' => ids, 'v' => Array(variants).uniq.sort },
                        expires_at: Time.zone.at(bucket_start + TTL.to_i))
    end

    # Returns the granted sets, or nil for anything we did not issue or issued
    # too long ago.
    #
    # The expiry rides inside the signed payload, so a grant cannot be extended
    # by editing it.
    def verify(token)
      payload = verifier.verified(token.to_s)
      return nil unless payload.is_a?(Hash)

      { ids: Array(payload['i']).to_set, variants: Array(payload['v']).to_set }
    end

    private

    # WHY THERE IS NO ADDRESS IN HERE, since binding one would be the obvious
    # thing and the first draft did it.
    #
    # It cannot work while these pages are microcached. The first reader in a
    # five-minute window would mint the grant, nginx would serve that same HTML
    # to everyone behind them, and every one of those readers would present a
    # grant bound to somebody else's address and be refused -- a blank wall for
    # everybody but the first, which is precisely the failure this work must
    # not cause. Uncaching the wall to save the binding would hand back the
    # performance #142 Phase 0 bought.
    #
    # What is lost is real and worth stating plainly: one fetched page yields a
    # grant any number of clients may use for its lifetime. What is NOT lost is
    # the mechanism -- a grant only ever names frames that page rendered, so a
    # fleet still has to fetch a page for every set of frames it wants, and
    # page fetches are ordinary HTTP requests that nginx already rate-limits,
    # counts and caches. That is the whole objective: put the harvest back in
    # front of the controls it was bypassing.
    def bucket_start
      (Time.now.to_i / CACHE_WINDOW) * CACHE_WINDOW
    end

    def verifier
      @verifier ||= Rails.application.message_verifier(:wall_grant)
    end
  end
end
