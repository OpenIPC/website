# frozen_string_literal: true

require 'test_helper'

# What the download step says under the file, besides the file (#190).
#
# The wizard is where the site knows most about a visitor -- the chip, the flash
# size, the edition -- and a camera is on their bench. Until now it said nothing
# about what they were accepting by flashing the image, and nothing to the
# integrator who is about to ship a product on it.
#
# These tests exist to hold three lines that are easy to get wrong in ways no
# one notices: a licence claim made about an image that does not contain the
# thing being licensed, a commercial ask put in front of someone reflashing one
# doorbell, and anything at all appearing between the command blocks.
class DownloadLicenceTest < ActionDispatch::IntegrationTest
  setup do
    @vendor = Vendor.create!(name: 'Licence Probe Vendor')
  end

  def soc_for(model:, status: 'done', segment: nil)
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

  # --- the licence claim ---

  test 'a done chip is told what licence the image carries' do
    wizard soc_for(model: 'PROBE1')

    assert_select '.download-licence', 1
    assert_match(/Prosperity Public License/, response.body)
  end

  # The one this file exists for. Twelve of the images the wizard can assemble
  # contain no Majestic at all -- checked against BR2_PACKAGE_MAJESTIC in every
  # defconfig -- and every one of them belongs to a chip that is not `done`.
  # Saying "this image includes Majestic" there would be a false statement
  # about what the visitor is flashing, on the one line whose whole job is to
  # be true about licensing.
  Soc::STATUS.keys.map(&:to_s).reject { |s| s == 'done' }.each do |status|
    test "a #{status} chip makes no claim about the licence" do
      wizard soc_for(model: "PROBE#{status.upcase}", status:)

      assert_select '.download-licence', 0
      assert_no_match(/Prosperity/, response.body)
    end
  end

  # --- who gets asked about business ---

  {
    'fpv' => 'Building an FPV product',
    'cctv' => 'Deploying more than a handful',
    'unknown' => 'Shipping a product'
  }.each do |segment, question|
    test "a #{segment} chip is asked the #{segment} question" do
      wizard soc_for(model: "PROBE#{segment.upcase}", segment:)

      assert_match(/#{Regexp.escape(question)}/, response.body)
      assert_select '.download-licence a[data-event=?]', "download-step:business:#{segment}"
    end
  end

  test 'a consumer chip is told the licence and asked nothing' do
    wizard soc_for(model: 'PROBECONS', segment: 'consumer')

    assert_select '.download-licence', 1
    assert_match(/Prosperity Public License/, response.body)
    # The licence sentence links the licence itself, so the business ask is
    # identified by its event rather than by being the only link in the block.
    assert_select '.download-licence a[data-event]', 0
  end

  # A column an admin types into. A value that is not a segment must not reach
  # a translation key, or the page publishes "translation missing" as its ask.
  test 'a segment nobody recognises falls back to the neutral question' do
    wizard soc_for(model: 'PROBEJUNK', segment: 'drone-ish')

    assert_no_match(/translation missing/i, response.body)
    assert_select '.download-licence a[data-event=?]', 'download-step:business:unknown'
  end

  test 'an unclassified chip falls back to the neutral question' do
    wizard soc_for(model: 'PROBENIL', segment: nil)

    assert_select '.download-licence a[data-event=?]', 'download-step:business:unknown'
  end

  # --- attribution ---

  # ?ref= rather than the event alone. The beacon is an async request and this
  # link navigates in the same tab, so the count can be cancelled; the landing
  # page reading ?ref= cannot be raced.
  test 'the business link carries its own attribution' do
    wizard soc_for(model: 'PROBEREF', segment: 'fpv')

    soc = Soc.find_by!(model: 'PROBEREF')
    href = css_select('.download-licence a[data-event]').first['href']
    query = Rack::Utils.parse_query(URI.parse(href).query)

    assert_equal 'download-step', query['ref']
    assert_equal soc.urlname, query['soc']
    assert_equal 'lite', query['edition']
  end

  test 'the business link keeps the visitor in their own language' do
    soc = soc_for(model: 'PROBERU', segment: 'fpv')
    wizard soc, locale: 'ru'

    assert_select '.download-licence a[href^=?]', '/ru/business'
  end

  # --- what must not change ---

  # The rule from #190: nothing stands between the person and the file, and
  # nothing lands between the command blocks they paste into a bootloader.
  #
  # Asserted structurally rather than by byte offset. The page already carries
  # <pre> blocks above this section for the backup step, so "the notice comes
  # before the first <pre> on the page" is a test that fails for a reason that
  # has nothing to do with the rule -- and the first version of this test did
  # exactly that.
  test 'it sits below the download link, with no command block between them' do
    wizard soc_for(model: 'PROBEPOS', segment: 'fpv')

    body = response.body
    link = body.index('download_full_image')
    notice = body.index('download-licence')

    assert link, 'no download link on the page'
    assert notice, 'no licence notice on the page'
    assert_operator link, :<, notice, 'the notice is above the download link'
    assert_not_includes body[link...notice], '<pre',
                        'a command block sits between the download link and the notice'
  end

  # And it stays in the link's own column, not in the one holding the commands.
  test 'it lives beside the download link, not among the commands' do
    wizard soc_for(model: 'PROBECOL', segment: 'fpv')

    assert_select '.github .download-licence', 1
    assert_select '.download-licence pre', 0
  end

  test 'the download link itself is untouched' do
    soc = soc_for(model: 'PROBELINK', segment: 'fpv')
    wizard soc

    link = css_select("a[href*='download_full_image']").first

    assert link, 'the download link is gone'
    assert_equal "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}/download_full_image" \
                 '?flash_size=8&flash_type=nor&fw_release=lite&layout=8',
                 link['href']
  end
end
