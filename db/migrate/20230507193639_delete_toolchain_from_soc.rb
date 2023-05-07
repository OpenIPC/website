class DeleteToolchainFromSoc < ActiveRecord::Migration[7.0]
  def change
    remove_column :socs, :toolchain_filename
  end
end
