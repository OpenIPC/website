class CreateSensors < ActiveRecord::Migration[7.0]
  def change
    create_table :sensors do |t|
      t.string :urlname
      t.references :vendor
      t.string :model
      t.string :mode
      t.string :optical_format
      t.string :imager_size
      t.string :active_pixels
      t.string :pixel_size
      t.string :color_filter_array
      t.string :max_data_rate
      t.string :max_fps_full
      t.string :max_fps_vga
      t.string :adc_resolution
      t.string :responsivity
      t.string :pixel_dynamic_range
      t.string :snr_max
      t.string :voltage
      t.string :power_consumption
      t.string :operating_temp
      t.string :packaging
      t.string :status
      t.text :notes
      t.timestamps
      t.index :urlname, unique: true
    end
  end
end
