class DropCustomCommands < ActiveRecord::Migration[7.0]
  def change
    drop_table :custom_commands
  end
end
