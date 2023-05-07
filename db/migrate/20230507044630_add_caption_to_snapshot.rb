class AddCaptionToSnapshot < ActiveRecord::Migration[7.0]
  def change
    add_column :snapshots, :caption, :string
  end
end
