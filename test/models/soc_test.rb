# frozen_string_literal: true

require 'test_helper'

# Soc.find is overridden to resolve the urlname slug that to_param produces.
# These pin the half that was missing: what it does when nothing matches.
class SocTest < ActiveSupport::TestCase
  setup do
    @vendor = Vendor.create!(name: 'Testco')
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300')
  end

  test 'generates a slug from the model and addresses the record by it' do
    assert_equal 'ts3516ev300', @soc.urlname
    assert_equal 'ts3516ev300', @soc.to_param
    assert_equal @soc, Soc.find('ts3516ev300')
  end

  test 'still finds by id, for old links and the admin forms' do
    assert_equal @soc, Soc.find(@soc.id)
  end

  test 'raises RecordNotFound for a slug that does not exist' do
    # /cameras/vendors/ingenic/socs/t31 used to answer 500 here: find returned
    # nil and Firmware#generate then called uboot_file on it. There is no SoC
    # called plain "t31" -- the real ones are t31a, t31x, t31n and so on.
    assert_raises(ActiveRecord::RecordNotFound) { Soc.find('no-such-soc') }
  end

  test 'raises RecordNotFound for a blank identifier' do
    assert_raises(ActiveRecord::RecordNotFound) { Soc.find(nil) }
    assert_raises(ActiveRecord::RecordNotFound) { Soc.find('') }
  end

  test 'find_by_param answers nil instead of raising' do
    assert_equal @soc, Soc.find_by_param('ts3516ev300')
    assert_nil Soc.find_by_param('no-such-soc')
    assert_nil Soc.find_by_param(nil)
    assert_nil Soc.find_by_param('')
  end
end
