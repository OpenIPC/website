class AddToolchainToSocs < ActiveRecord::Migration[7.0]
  def change
    add_column :socs, :toolchain_filename, :string
  end
end
