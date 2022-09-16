class AddIndexesToSnapshots < ActiveRecord::Migration[7.0]
  def change
    add_index :snapshots, :mac_address
    add_index :snapshots, :ip_address
    add_index :snapshots, :soc
    add_index :snapshots, :sensor
    add_index :snapshots, :flash_size
    add_index :snapshots, :created_at
  end
end
