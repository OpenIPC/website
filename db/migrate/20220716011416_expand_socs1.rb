class ExpandSocs1 < ActiveRecord::Migration[7.0]
  def change
    add_column :socs, :urlname, :string
    add_index :socs, :urlname, unique: true
  end
end
