# frozen_string_literal: true

# One row per firmware image sent. The site has never recorded a download, so
# the only evidence of what people flash is nginx's access log, which keeps
# fourteen days -- there has never been a monthly figure and none can be
# recovered after the fact. See issue #83.
#
# Deliberately not a counter column on socs: the questions worth asking are
# which flash sizes and editions people actually take, and how that moves, and
# a total answers none of them.
class CreateDownloads < ActiveRecord::Migration[7.0]
  def change
    create_table :downloads do |t|
      # No foreign key. A SoC that is deleted should not take the record of
      # what people downloaded with it, and soc_model keeps the row readable
      # once the id means nothing.
      t.references :soc, null: true, index: true
      t.string :soc_model, null: false
      t.string :flash_type, null: false
      t.string :release, null: false
      t.integer :flash_size
      t.integer :bytes
      t.datetime :created_at, null: false
    end

    # What a report actually groups by.
    add_index :downloads, :created_at
    add_index :downloads, %i[soc_model created_at]
  end
end
