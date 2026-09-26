# frozen_string_literal: true

# The sensors table never had a model (#290). Vendor declared `has_many
# :sensors`, but with no Sensor class the association could not be loaded, and
# db/seeds.rb called `Sensor.delete_all` -- so `db:seed` on an empty database
# stopped there with a NameError and never reached the lines after it.
#
# Safe under the deploy rule that a rollback restores the image and never the
# schema: no release has ever read this table, because none could.
#
# The block is the table as schema.rb last described it, so the migration
# reverses; nothing is lost that was ever read, but `db:rollback` should work.
class DropSensors < ActiveRecord::Migration[8.1]
  STRINGS = %i[active_pixels adc_resolution color_filter_array imager_size max_data_rate
               max_fps_full max_fps_vga mode model operating_temp optical_format packaging
               pixel_dynamic_range pixel_size power_consumption responsivity snr_max status
               urlname voltage].freeze

  def change
    drop_table :sensors do |t|
      STRINGS.each { |column| t.string column }
      t.text :notes
      t.bigint :vendor_id
      t.timestamps
      t.index :urlname, unique: true
      t.index :vendor_id
    end
  end
end
