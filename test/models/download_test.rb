# frozen_string_literal: true

require 'test_helper'
require 'minitest/mock'

class DownloadTest < ActiveSupport::TestCase
  setup do
    @vendor = Vendor.create!(name: 'Testco')
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300',
                       uboot_filename: 'u.bin', linux_filename: 'openipc.ts3516ev300-nor-lite.tgz')
    @fw = Firmware.new(size: 16, flash_type: 'nor', release: 'lite', soc: @soc)
  end

  test 'records what was actually sent' do
    d = Download.record(firmware: @fw, soc: @soc, bytes: 16.megabytes)

    assert_equal 'ts3516ev300', d.soc_model
    assert_equal 'nor', d.flash_type
    assert_equal 'lite', d.release
    assert_equal 16, d.flash_size
    assert_equal 16.megabytes, d.bytes
    assert_equal @soc.id, d.soc_id
    assert_not_nil d.created_at
  end

  test 'a nand download records its flash type' do
    fw = Firmware.new(size: 128, flash_type: 'nand', release: 'ultimate', soc: @soc)
    assert_equal 'nand', Download.record(firmware: fw, soc: @soc).flash_type
  end

  test 'failing to record never costs somebody their download' do
    # A full disk, a locked table, a migration not yet run on one container --
    # none of those are reasons to fail a request that has already produced a
    # valid image.
    Download.stub(:create!, ->(*) { raise ActiveRecord::StatementInvalid, 'table is gone' }) do
      assert_nothing_raised { assert_nil Download.record(firmware: @fw, soc: @soc) }
    end
  end

  test 'the row outlives the SoC it names' do
    d = Download.record(firmware: @fw, soc: @soc)
    @soc.destroy
    d.reload

    assert_equal 'ts3516ev300', d.soc_model, 'the model must stay readable'
    assert_nil Download.find(d.id).soc
  end

  test 'it groups by what a report would ask' do
    3.times { Download.record(firmware: @fw, soc: @soc) }
    Download.record(firmware: Firmware.new(size: 8, flash_type: 'nor', release: 'lite', soc: @soc), soc: @soc)

    assert_equal({ 16 => 3, 8 => 1 }, Download.group(:flash_size).count)
    assert_equal({ 'ts3516ev300' => 4 }, Download.group(:soc_model).count)
  end
end
