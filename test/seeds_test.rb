# frozen_string_literal: true

require 'test_helper'

# db/seeds.rb has to run to the end (#290). It called `Sensor.delete_all` for a
# table that never had a model, so `db:seed` stopped with a NameError halfway
# through on a fresh checkout. There is nothing left to seed since the
# catalogue became a file (#289), and what is left must still load.
class SeedsTest < ActiveSupport::TestCase
  test 'db/seeds.rb runs to the end' do
    assert_nothing_raised { load Rails.root.join('db/seeds.rb').to_s }
  end

  test 'nothing refers to the dropped sensors table' do
    assert_not Vendor.method_defined?(:sensors)
    assert_not ActiveRecord::Base.connection.table_exists?(:sensors)
  end
end
