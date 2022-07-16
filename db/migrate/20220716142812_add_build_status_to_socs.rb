class AddBuildStatusToSocs < ActiveRecord::Migration[7.0]
  def change
    add_column :socs, :build_status_url, :string
  end
end
