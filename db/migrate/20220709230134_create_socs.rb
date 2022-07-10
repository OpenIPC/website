class CreateSocs < ActiveRecord::Migration[7.0]
  def change
    create_table :socs do |t|
      t.references :vendor
      t.string :family
      t.string :model
      t.string :version
      t.string :status
      t.string :load_address
      t.string :sdk
      t.string :kernel
      t.string :uboot_filename
      t.string :linux_filename
      t.text :notes
      t.timestamps
    end
  end
end
