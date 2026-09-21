# frozen_string_literal: true

# What kind of product this chip ends up in (#190).
#
# The download step says something different to someone building an FPV VTX,
# someone deploying a hundred CCTV cameras and someone reflashing one doorbell,
# and the site had no way to tell them apart. Additive and nullable, so a
# rollback leaves the column behind and nothing reads it -- Soc#segment_name
# treats a null as `unknown`, which is the generic copy.
#
# The classification itself lives on the model, because a migration only ever
# runs against a database that already has rows: a schema-loaded setup never
# executes this one, and db/seeds.rb has to be able to make the same call.
class AddSegmentToSocs < ActiveRecord::Migration[8.1]
  def up
    add_column :socs, :segment, :string
    Soc.reset_column_information
    Soc.classify_segments!
  end

  def down
    remove_column :socs, :segment
  end
end
