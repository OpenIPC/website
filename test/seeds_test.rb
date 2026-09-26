# frozen_string_literal: true

require 'test_helper'

# db/seeds.rb has to run to the end (#290). It called `Sensor.delete_all` for a
# table that never had a model, so `db:seed` stopped with a NameError halfway
# through and nothing after that line -- the segment classification among it --
# ever ran on a fresh checkout. Loaded inside the test's transaction, so the
# fixtures come back afterwards.
class SeedsTest < ActiveSupport::TestCase
  test 'db/seeds.rb runs to the end' do
    load Rails.root.join('db/seeds.rb').to_s

    assert Soc.where.not(segment: nil).exists?, 'the seed stopped before classifying anything'
  end

  test 'nothing declares an association to the dropped sensors table' do
    assert_nil Vendor.reflect_on_association(:sensors)
    assert_not ActiveRecord::Base.connection.table_exists?(:sensors)
  end
end
