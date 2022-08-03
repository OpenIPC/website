class CreateSnapshots < ActiveRecord::Migration[7.0]
  def change
    create_table :snapshots do |t|
      t.string :mac_address
      t.string :ip_address
      t.string :hostname
      t.string :soc
      t.string :sensor
      t.string :flash_size
      t.string :firmware
      t.string :uptime
      t.string :soc_temperature

      t.timestamps
    end
  end
end
