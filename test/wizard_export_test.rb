# frozen_string_literal: true

require 'test_helper'
require 'wizard_export'

# The wizard's output as data (#163).
#
# The point of the export is that nobody re-derives flash geometry in a second
# language: Rails renders its own command lines and both halves of the site
# read the result. So what these check is not what the commands say -- the
# wizard's own 73 tests and camera_test.rb's 40 do that -- but that the export
# carries them faithfully, carries nothing a renderer would have to interpret,
# and leaves the three per-visitor values as holes.
class WizardExportTest < ActiveSupport::TestCase
  setup do
    @vendor = Vendor.create!(name: 'Testco')
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300', status: 'done',
                       load_address: '0x82000000',
                       uboot_filename: 'u-boot-ts3516ev300-universal.bin',
                       linux_filename: 'openipc.ts3516ev300-nor-lite.tgz')
  end

  def document
    @document ||= WizardExport.document(@soc)
  end

  test 'it enumerates combinations and describes each one fully' do
    combinations = document['combinations']

    assert_operator combinations.size, :>, 20, 'the menu offers more than this'
    combinations.each do |entry|
      assert_equal %w[blocks edition flash_size flash_type layout_size network_interface
                      partition_layout sd_card_slot warnings].sort,
                   entry.keys.sort
      assert_includes Camera::FLASH_CHIP, entry['flash_type']
      assert_includes Camera::NET_IFACE, entry['network_interface']
      assert_includes Camera::SD_CARD, entry['sd_card_slot']
      assert_equal WizardExport::BLOCKS.sort, entry['blocks'].keys.sort
    end
  end

  test 'a block is lines, notes and a paste warning -- and no markup' do
    # Markup in data is markup two renderers then have to agree about, and the
    # do-not-paste warning is the one line on this page that must be rendered
    # in the reader's own language.
    document['combinations'].each do |entry|
      entry['blocks'].each do |name, block|
        assert_equal %w[lines no_paste notes].sort, block.keys.sort, name

        block['lines'].each do |line|
          assert_kind_of String, line
          assert_no_match(/<[a-z]/, line, "#{name} carries markup: #{line}")
        end

        block['notes'].each { |note| assert_kind_of String, note }
        assert_includes [true, false], block['no_paste']
      end
    end
  end

  test 'the three per-visitor values are holes, not values' do
    # They appear only inside setenv and in the backup filename, so they can be
    # holes -- and if one ever stops being a hole, an address from whoever ran
    # the export ships to every visitor.
    all = document['combinations'].flat_map { |e| e['blocks'].values.flat_map { |b| b['lines'] } }
    joined = all.join("\n")

    assert_includes joined, WizardExport::IPADDR
    assert_includes joined, WizardExport::SERVERIP
    assert_no_match(/\b192\.168\.\d+\.\d+\b/, joined, 'a real IP address reached the export')
    assert_no_match(/\b(?:[0-9a-f]{2}:){5}[0-9a-f]{2}\b/i, joined, 'a real MAC reached the export')
  end

  test 'the export says what the helper says' do
    # One producer. If these ever disagree, the page and the data disagree, and
    # the data is what the static wizard will render.
    view = ApplicationController.new.tap { |c| c.request = ActionDispatch::TestRequest.create }
                                .view_context
    entry = document['combinations'].first

    camera = Camera.new(
      camera_ip_address: WizardExport::IPADDR, server_ip_address: WizardExport::SERVERIP,
      camera_mac_address: WizardExport::ETHADDR, flash_type: entry['flash_type'],
      firmware_version: entry['edition'], network_interface: entry['network_interface'],
      sd_card_slot: entry['sd_card_slot']
    )
    camera.partition_layout = entry['partition_layout']
    camera.soc = @soc

    WizardExport::BLOCKS.each do |block|
      wanted = view.public_send("#{block}_lines", camera).map(&:to_s).reject { |l| l.start_with?('<') }

      assert_equal wanted, entry['blocks'][block]['lines'], block
    end
  end

  test 'the exported patterns are the ones the form uses, and the stricter of the two' do
    # A form that accepts what the server refuses fails on submit with nothing
    # said, so the browser's pattern has to be at least as strict as Ruby's.
    patterns = document['patterns']
    mac = Regexp.new(patterns['mac'])
    ip = Regexp.new(patterns['ip'])

    %w[aa:bb:cc:dd:ee:ff AA-BB-CC-DD-EE-FF].each do |value|
      assert_match mac, value
      assert_match MAC_ADDRESS_FORMAT, value, "the browser accepts #{value} and the server does not"
    end
    ['aa:bb:cc:dd:ee', 'zz:bb:cc:dd:ee:ff', ''].each { |value| assert_no_match mac, value }

    %w[192.168.1.10 10.0.0.1 255.255.255.255].each do |value|
      assert_match ip, value
      assert_match IP_ADDRESS_FORMAT, value, "the browser accepts #{value} and the server does not"
    end
    ['256.1.1.1', '1.2.3', 'not-an-ip'].each { |value| assert_no_match ip, value }
  end

  # The four the controller special-cases, which the issue names as the ones
  # worth a golden test: two answer with a different page, and two turn on
  # what upstream has published rather than on geometry.

  test 'SigmaStar on NAND is a page of its own, not generated commands' do
    vendor = Vendor.create!(name: 'SigmaStar')
    soc = Soc.create!(vendor: vendor, model: 'SSC338Q', status: 'done', load_address: '0x20000000',
                      uboot_filename: 'u-boot-ssc338q-universal.bin',
                      linux_filename: 'openipc.ssc338q-nor-lite.tgz')

    nand = WizardExport.document(soc)['combinations'].select { |e| e['flash_type'] == 'nand' }

    assert_not_empty nand
    nand.each do |entry|
      assert_equal 'sigmastar_nand', entry['page']
      assert_nil entry['blocks'], 'a page with its own procedure must not carry generated commands'
    end

    nor = WizardExport.document(soc)['combinations'].reject { |e| e['flash_type'] == 'nand' }
    nor.each { |entry| assert_nil entry['page'], 'NOR is the ordinary wizard for SigmaStar' }
  end

  test 'the HiSilicon NVRs are a page of their own on every flash type' do
    %w[HI3536CV100 HI3536DV100].each do |model|
      vendor = Vendor.create!(name: "NVRco#{model}")
      soc = Soc.create!(vendor: vendor, model: model, status: 'done', load_address: '0x82000000',
                        uboot_filename: '', linux_filename: '')

      WizardExport.document(soc)['combinations'].each do |entry|
        assert_equal 'hi3536dv100', entry['page'], model
      end
    end
  end

  test '8MB with no Lite build warns that the chip is too small' do
    # hi3516cv6xx and hi3519dv500 are the real ones: published as Ultimate and
    # nothing else, so there is no Lite to fall back to and the advice is the
    # chip rather than the edition.
    soc = @soc
    soc.define_singleton_method(:available_releases) { |_type| ['ultimate'] }

    entries = WizardExport.document(soc)['combinations']
                          .select { |e| e['partition_layout'] == 'nor8m' && e['edition'] == 'ultimate' }

    assert_not_empty entries, 'Ultimate on the 8MB layout is offered when there is no Lite'
    on_chip = entries.select { |e| e['flash_type'] == 'nor8m' }
    on_bigger = entries.reject { |e| e['flash_type'] == 'nor8m' }

    on_chip.each { |e| assert_includes e['warnings'], 'no_lite_chip' }
    on_bigger.each { |e| assert_includes e['warnings'], 'no_lite_layout' }
  end

  test 'a NAND-only part says nothing is published for NOR' do
    soc = @soc
    soc.define_singleton_method(:available_releases) { |type| type == 'nand' ? ['ultimate'] : [] }

    document = WizardExport.document(soc)
    nor = document['combinations'].select { |e| e['flash_type'].start_with?('nor') }

    # Nothing published means no edition to enumerate, so the combination is
    # not offered at all -- which is the menu's behaviour too.
    assert_empty nor, 'NOR combinations were enumerated for a part with no NOR build'
    assert_not_empty document['combinations'].select { |e| e['flash_type'] == 'nand' }
  end

  test 'it carries the editions upstream publishes, per flash type' do
    assert_equal Soc::FLASH_TYPES.sort, document['editions'].keys.sort
    document['editions'].each_value { |list| assert_kind_of Array, list }
  end
end
