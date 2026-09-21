# frozen_string_literal: true

require 'test_helper'

# What the success block offers once the camera is up (#191).
#
# It is the closest thing this site has to a thank-you page and it ended the
# visit with nothing to do. It is also the only place in the wizard where an
# ask is honest: after the value has been delivered, never before the download,
# and never on a page that cannot install anything.
#
# These hold the part that is easy to get wrong quietly: which ask a given
# visitor is shown, and that they are never shown both.
class WizardWhatNextTest < ActionDispatch::IntegrationTest
  setup { @vendor = Vendor.create!(name: 'WhatNext Probe Vendor') }

  def soc_for(model:, segment: nil, status: 'done')
    Soc.create!(vendor: @vendor, model:, family: 'probe', status:, segment:,
                uboot_filename: 'u-boot-probe.bin', linux_filename: 'probe-nor-lite.tgz')
  end

  def wizard(soc, locale: nil)
    prefix = locale ? "/#{locale}" : ''
    get "#{prefix}/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}",
        params: { camera: { flash_type: 'nor8m', firmware_version: 'lite',
                            network_interface: 'eth', sd_card_slot: 'nosd',
                            camera_ip_address: '192.168.1.10',
                            server_ip_address: '192.168.1.254' } }
    assert_response :success
  end

  # Direct children only: the chat item now carries a nested list of rooms, and
  # a bare `li` would count those too. The assertion means what it always did.
  test 'the block offers three things to do next' do
    wizard soc_for(model: 'WNBASE')

    assert_select '.alert-success [data-whatnext] > li', 3
  end

  # The whole point of the placement. An ask above the download is a toll; this
  # one is under the thing the visitor came for, inside the block that says the
  # camera is up.
  #
  # Not asserted as "no command block follows it": the collapsed expert section
  # further down has command blocks of its own and always will. What matters is
  # that it is after the download step's, which is what being inside the
  # success block means.
  test 'it sits inside the success block, after the download step' do
    wizard soc_for(model: 'WNPOS')

    assert_select '.alert-success [data-whatnext]', 1
    body = response.body

    assert_operator body.index('download_full_image'), :<, body.index('data-whatnext')
    assert_operator body.index('alert alert-success'), :<, body.index('data-whatnext')
  end

  # --- the third item is the segment's ask, and only ever one of them ---

  { 'consumer' => 'download-step:donate',
    'unknown' => 'download-step:donate',
    'fpv' => 'download-step:business:fpv',
    'cctv' => 'download-step:business:cctv' }.each do |segment, event|
    test "a #{segment} chip is asked for #{event.split(':').last}" do
      wizard soc_for(model: "WN#{segment.upcase}", segment:)

      assert_select '[data-whatnext] a[data-event=?]', event, 1
    end
  end

  # Donate and commercial terms in the same list would read as a menu of ways
  # to pay. A doorbell owner is not a lead; an integrator flashing a hundred
  # cameras is not someone to pass a tip jar to.
  test 'nobody is asked for money twice' do
    %w[fpv cctv consumer unknown].each do |segment|
      wizard soc_for(model: "WNBOTH#{segment.upcase}", segment:)

      asks = css_select('[data-whatnext] a[data-event]')
             .map { |a| a['data-event'] }
             .grep(/donate|business/)

      assert_equal 1, asks.size, "#{segment} is shown #{asks.inspect}"
    end
  end

  # --- the chat doors: every room is offered, the order is what varies ---

  EN_ROOM = 'https://t.me/+7LL2kc32SOo5YWYy'
  FPV_ROOM = 'https://t.me/+BMyMoolVOpkzNWUy'
  RU_ROOM = 'https://t.me/+Sl2GPoR9G2iJAOCr'

  def chat_hrefs
    css_select('[data-whatnext] [data-chat] a').map { |a| a['href'] }
  end

  # The rule, and the reason it replaced routing on the locale: a language
  # setting is not a network. `zh` used to be sent to /community instead of a
  # room, on the belief that the page carried a WeChat contact -- it never did,
  # so that swapped one unopenable link for a page of four. Ordering on the
  # locale is honest; removing a door on it is not.
  test 'every visitor is offered every room' do
    soc = soc_for(model: 'WNDOORS')
    seen = {}

    [nil, 'ru', 'zh'].each do |locale|
      wizard soc, locale: locale
      seen[locale] = chat_hrefs.sort
    end

    assert_equal [EN_ROOM, FPV_ROOM, RU_ROOM].sort, seen[nil]
    assert_equal 1, seen.values.uniq.size,
                 "a locale is shown a different set of rooms: #{seen.inspect}"
  end

  # Leads with, still offers, in that order -- the shape donation_path_test.rb
  # already locks for the PayWall/Open Collective split.
  test 'the locale decides the order, and only the order' do
    soc = soc_for(model: 'WNORDER')

    wizard soc, locale: 'ru'

    assert_equal RU_ROOM, chat_hrefs.first, 'the Russian room does not lead for a Russian reader'
    assert_includes chat_hrefs, EN_ROOM, 'the English room was taken away instead of reordered'

    wizard soc

    assert_equal EN_ROOM, chat_hrefs.first
    assert_includes chat_hrefs, RU_ROOM
  end

  # The chip is the more specific signal, so it wins the ordering over the
  # language -- that part of #191 is unchanged.
  test 'an FPV chip is offered the FPV room first' do
    soc = soc_for(model: 'WNFPVROOM', segment: 'fpv')

    wizard soc

    assert_equal FPV_ROOM, chat_hrefs.first

    wizard soc, locale: 'ru'

    assert_equal FPV_ROOM, chat_hrefs.first, 'the language overrode the chip'
  end

  # The defect this replaces: the FPV branch ran before the locale check and
  # ignored it, so an FPV chip handed a Chinese visitor a t.me link -- the one
  # thing the zh fallback existed to prevent, and untested because the FPV test
  # ran in the default locale.
  #
  # Asserted as "the way out comes first", not "there is no t.me link". Removing
  # the room because the page is in Chinese is the same mistake pointing the
  # other way: it strands the Shenzhen FPV builder whose VPN works.
  test 'a Chinese visitor is never handed Telegram first, FPV chip included' do
    [[nil, 'WNZHPLAIN'], %w[fpv WNZHFPV]].each do |segment, model|
      wizard soc_for(model:, segment:), locale: 'zh'
      body = response.body

      assert_select '[data-whatnext] [data-chat-fallback]', 1
      assert_operator body.index('data-chat-fallback'), :<, body.index('data-chat='),
                      "#{model}: the rooms come before the way out for a Chinese reader"
      assert_includes chat_hrefs, FPV_ROOM, "#{model}: a room was removed rather than reordered"
    end
  end

  # A door nobody can tell apart in the dashboard is a door whose evidence is
  # not there in four weeks.
  test 'each room is counted apart' do
    wizard soc_for(model: 'WNEVENTS')

    events = css_select('[data-chat] a[data-event]').map { |a| a['data-event'] }

    assert_equal %w[download-step:chat:en download-step:chat:fpv download-step:chat:ru], events.sort
  end

  test 'the way out is named in every locale' do
    soc = soc_for(model: 'WNWAYOUT')

    [nil, 'ru', 'zh'].each do |locale|
      wizard soc, locale: locale

      assert_select '[data-chat-fallback] a[data-event=?]', 'download-step:chat:issues', 1
      assert_no_match(/translation missing/i, response.body)
    end
  end

  test 'every link stays in the visitor language' do
    wizard soc_for(model: 'WNRU', segment: 'consumer'), locale: 'ru'

    assert_select '[data-whatnext] a[href="/ru/open-wall"]', 1
    assert_select '[data-whatnext] a[href="/ru/donate"]', 1
    assert_no_match(/translation missing/i, response.body)
  end

  # --- the page that cannot install anything ---

  # #191's other half: a donate link on a page that installs nothing is the
  # pattern this issue exists to end.
  test 'the not-ready page no longer asks for money' do
    %w[/ /ru /zh].each do |prefix|
      soc = Soc.create!(vendor: @vendor, model: "WNNOTREADY#{prefix.delete('/')}",
                        status: 'rnd', uboot_filename: '', linux_filename: '')
      get "#{prefix == '/' ? '' : prefix}/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

      assert_response :success
      # Scoped to the page. The footer links /donate on every page of the site
      # and always has; this is about the page's own copy.
      assert_select 'main a[href$="/donate"]', 0,
                    "#{prefix} still asks a visitor it cannot help for money"
    end
  end
end
