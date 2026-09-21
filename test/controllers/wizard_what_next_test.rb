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

  test 'the block offers three things to do next' do
    wizard soc_for(model: 'WNBASE')

    assert_select '.alert-success [data-whatnext] li', 3
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

      assert_select "[data-whatnext] a[data-event=?]", event, 1
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

  # --- the chat link goes somewhere the visitor can actually reach ---

  test 'the chat link follows the locale' do
    soc = soc_for(model: 'WNCHAT')

    wizard soc
    assert_select '[data-whatnext] a[data-event="download-step:chat"][href^=?]', 'https://t.me/'

    wizard soc, locale: 'ru'
    ru = css_select('[data-whatnext] a[data-event="download-step:chat"]').first['href']

    wizard soc
    en = css_select('[data-whatnext] a[data-event="download-step:chat"]').first['href']

    assert_not_equal en, ru, 'Russian and English visitors are sent to the same room'
  end

  # Telegram is blocked in China, so a Telegram link there is a link that does
  # not open. /community lists the WeChat contact alongside the rest.
  test 'Chinese visitors are not sent to Telegram' do
    wizard soc_for(model: 'WNZH'), locale: 'zh'

    href = css_select('[data-whatnext] a[data-event="download-step:chat"]').first['href']

    assert_not_includes href, 't.me'
    assert_equal '/zh/community', href
  end

  # An FPV chip's questions get answered in the FPV room, and the page already
  # knows which chip it is.
  test 'an FPV chip points at the FPV room' do
    wizard soc_for(model: 'WNFPVROOM', segment: 'fpv')

    href = css_select('[data-whatnext] a[data-event="download-step:chat"]').first['href']

    assert_equal 'https://t.me/+BMyMoolVOpkzNWUy', href
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
