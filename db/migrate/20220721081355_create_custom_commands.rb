class CreateCustomCommands < ActiveRecord::Migration[7.0]
  def change
    create_table :custom_commands do |t|
      t.references :soc
      t.string :command_block
      t.text :text
      t.timestamps
    end
  end
end
