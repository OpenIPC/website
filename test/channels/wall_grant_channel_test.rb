# frozen_string_literal: true

require 'test_helper'

# The gate that decides whether a socket may receive anything at all.
#
# This exists because of a measurement, not a theory. In the first four hours
# after #267 moved frames onto the channel, 991 of the 1,055 addresses on it
# had requested NOTHING else -- no page, no asset, no favicon, only
# `GET /api/v1/wall/cable` -- and they took 2,393 MiB of the 2,437. They were
# speaking the ActionCable protocol directly, 984 behind one spoofed
# user-agent, and 196 connections took exactly MAX_PER_REQUEST frames. Not one
# came near FRAME_BUDGET, because the fleet uses each address once.
#
# So the budget is not the thing under test here; it never could stop that. The
# thing under test is that a client which never rendered a page cannot be
# served, and that a client which did cannot reach beyond what it was shown.
class WallGrantChannelTest < ActionCable::Channel::TestCase
  tests WallChannel

  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b
  ADDRESS = '203.0.113.50'

  setup do
    @cache = Rails.cache
    Rails.cache = ActiveSupport::Cache::MemoryStore.new

    stub_connection connection_id: 'abcdef0123456789', client_ip: ADDRESS
    @shown = Array.new(2) { write_frame }
    @hidden = write_frame
  end

  teardown do
    (@shown + [@hidden]).each { |id| WallImage.purge(id) }
    Rails.cache = @cache
  end

  def write_frame
    id = Snapshot.generate_public_id
    WallImage.store_bytes(id, :thumb, MINIMAL_JPEG)
    id
  end

  def frames_from(transmissions)
    transmissions.filter_map { |t| t['frame'] || t[:frame] }
  end

  def grant_for(ids, variants: %w[thumb])
    WallGrant.issue(pairs: ids.product(Array(variants)).map { |i, v| WallGrant.pair(i, v) })
  end

  test 'a socket that never rendered a page is refused' do
    subscribe

    assert subscription.rejected?, <<~MESSAGE.chomp
      A subscription with no grant was accepted. This is the whole issue: 94%
      of the clients measured on this channel had loaded no page at all, and
      an accepted subscription is all they needed.
    MESSAGE
  end

  test 'a forged grant is refused' do
    subscribe(grant: 'not.a.real.grant')

    assert subscription.rejected?
  end

  # There is deliberately NO test that a grant is refused for another address,
  # because grants are not bound to one. They cannot be: wall pages are
  # microcached for 300 seconds on a key with no address in it, so the first
  # reader's HTML is what every later reader receives, and a bound grant would
  # blank the wall for all of them.
  #
  # What replaces that property is this: a grant must be byte-identical across
  # renders inside the cache window, or the cached body is a coin toss between
  # two different grants and half the readers get one minted for a page they
  # are not looking at.
  test 'a grant is stable across renders, because the page it rides on is cached' do
    first = grant_for(@shown)
    second = grant_for(@shown)

    assert_equal first, second, <<~MESSAGE.chomp
      Two renders of the same frames produced different grants. nginx caches
      one of the two bodies for five minutes and serves it to everybody, so
      per-render entropy here is not harmless variation -- it is a different
      page for half the readers.
    MESSAGE
  end

  test 'a grant for different frames is a different grant' do
    assert_not_equal grant_for(@shown), grant_for([@hidden])
  end

  test 'an expired grant is refused' do
    token = grant_for(@shown)

    travel WallGrant::TTL + 1.minute do
      subscribe(grant: token)

      assert subscription.rejected?
    end
  end

  test 'a granted socket gets exactly the frames its page rendered' do
    subscribe(grant: grant_for(@shown))
    perform :request_frames, 'variant' => 'thumb', 'ids' => @shown

    assert_equal @shown.size, frames_from(transmissions).size
  end

  # The reason the grant is scoped to ids rather than simply proving
  # page-ness. A token that merely said "this client loaded a page" would let
  # one page fetch unlock the whole wall.
  test 'a granted socket cannot reach an id its page never showed' do
    subscribe(grant: grant_for(@shown))
    perform :request_frames, 'variant' => 'thumb', 'ids' => [@hidden]

    assert_empty frames_from(transmissions), <<~MESSAGE.chomp
      An id outside the grant was served. Enumeration is supposed to be
      impossible here, not merely metered -- that was the budget's job and it
      is what failed.
    MESSAGE
  end

  test 'asking for a mix yields only the granted part' do
    subscribe(grant: grant_for(@shown))
    perform :request_frames, 'variant' => 'thumb', 'ids' => @shown + [@hidden]

    assert_equal @shown.size, frames_from(transmissions).size
  end

  test 'a variant the page never rendered is refused' do
    WallImage.store_bytes(@shown.first, :fullhd, MINIMAL_JPEG)
    subscribe(grant: grant_for(@shown, variants: %w[thumb]))
    perform :request_frames, 'variant' => 'fullhd', 'ids' => [@shown.first]

    assert_empty frames_from(transmissions)
  ensure
    WallImage.purge(@shown.first)
  end

  # The cross-product hole, which is why a grant names PAIRS and not an id set
  # beside a variant set.
  #
  # A snapshot page renders its hero at fullhd and its archive tiles at icon2.
  # Checked separately, those two facts combine: the fullhd permission earned
  # by the hero could be spent on any tile's id, returning a full-resolution
  # view of a camera the reader had only ever been shown at 240x135. Nothing
  # about either half looks wrong on its own, which is what makes it worth a
  # test of its own.
  test 'a variant granted for one frame cannot be spent on another' do
    hero, tile = @shown
    WallImage.store_bytes(tile, :fullhd, MINIMAL_JPEG)

    subscribe(grant: WallGrant.issue(pairs: [WallGrant.pair(hero, 'fullhd'),
                                             WallGrant.pair(tile, 'thumb')]))
    perform :request_frames, 'variant' => 'fullhd', 'ids' => [tile]

    assert_empty frames_from(transmissions), <<~MESSAGE.chomp
      A frame was served at a size its page never drew it at. The grant held
      fullhd for the hero and thumb for the tile; pairing them separately lets
      a reader promote any thumbnail to full resolution.
    MESSAGE
  ensure
    WallImage.purge(tile) if tile
  end

  # Turbo Drive keeps one socket across navigations, so a second page has to be
  # able to hand over its own grant without tearing the connection down.
  test 'a later page adds its frames to the same socket' do
    subscribe(grant: grant_for(@shown))
    perform :use_grant, 'grant' => grant_for([@hidden])
    perform :request_frames, 'variant' => 'thumb', 'ids' => [@hidden]

    assert_equal 1, frames_from(transmissions).size
  end

  # And the half that matters for readers rather than for the harvest: a lazy
  # turbo-frame lands mid-way through the outer page's chunked requests, so
  # accepting its grant must not cancel what the page is still asking for.
  # Replacing rather than merging made half a gallery fail to paint.
  test 'a later grant does not revoke the frames still arriving' do
    subscribe(grant: grant_for(@shown))
    perform :use_grant, 'grant' => grant_for([@hidden])
    perform :request_frames, 'variant' => 'thumb', 'ids' => @shown

    assert_equal @shown.size, frames_from(transmissions).size, <<~MESSAGE.chomp
      The outer page's frames stopped being served once the archive frame
      handed over its grant. A reader would watch the gallery half-paint.
    MESSAGE
  end

  test 'an invalid grant mid-session stops the socket serving anything more' do
    subscribe(grant: grant_for(@shown))
    perform :use_grant, 'grant' => 'rubbish'
    perform :request_frames, 'variant' => 'thumb', 'ids' => @shown

    assert_empty frames_from(transmissions)
  end

  # The switch exists because a wrong grant blanks the wall for everyone, which
  # WallChannel's own header calls the one failure this work must not cause. It
  # has to actually work, and it has to be off by default.
  test 'the kill switch is off unless the environment says otherwise' do
    subscribe

    assert subscription.rejected?
  end

  test 'the kill switch restores service without a deploy' do
    ENV['WALL_GRANTS_DISABLED'] = '1'
    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => @shown

    assert_not subscription.rejected?
    assert_equal @shown.size, frames_from(transmissions).size
  ensure
    ENV.delete('WALL_GRANTS_DISABLED')
  end
end
