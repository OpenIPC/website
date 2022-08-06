class AddFeaturedToSocs < ActiveRecord::Migration[7.0]
  def change
    add_column :socs, :featured, :boolean, null: false, default: false, index: true
  end
end
