class AddStreamerToSnapshots < ActiveRecord::Migration[7.0]
  def change
    add_column :snapshots, :streamer, :string, index: true
  end
end
