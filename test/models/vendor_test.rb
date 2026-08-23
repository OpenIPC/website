# frozen_string_literal: true

require 'test_helper'

class VendorTest < ActiveSupport::TestCase
  setup do
    @vendor = Vendor.create!(name: 'Test Vendor')
  end

  test 'generates a slug from the name and addresses the record by it' do
    assert_equal 'test-vendor', @vendor.urlname
    assert_equal 'test-vendor', @vendor.to_param
    assert_equal @vendor, Vendor.find('test-vendor')
  end

  test 'still finds by id' do
    assert_equal @vendor, Vendor.find(@vendor.id)
  end

  test 'raises RecordNotFound for a slug that does not exist' do
    assert_raises(ActiveRecord::RecordNotFound) { Vendor.find('no-such-vendor') }
  end

  # The supported-hardware index takes ?vendor= as an optional filter, so it
  # asks with find_by_param and renders the unfiltered list when nothing is
  # given. If it used find, the plain page would 404.
  test 'find_by_param answers nil instead of raising' do
    assert_equal @vendor, Vendor.find_by_param('test-vendor')
    assert_nil Vendor.find_by_param('no-such-vendor')
    assert_nil Vendor.find_by_param(nil)
    assert_nil Vendor.find_by_param('')
  end
end
