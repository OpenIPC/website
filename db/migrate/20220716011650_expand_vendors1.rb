class ExpandVendors1 < ActiveRecord::Migration[7.0]
  def change
    add_column :vendors, :urlname, :string
    add_column :vendors,  :full_name, :string
    add_column :vendors,  :website_url, :string
    add_column :vendors, :notes, :text

    add_index :vendors, :urlname, unique: true
  end
end
