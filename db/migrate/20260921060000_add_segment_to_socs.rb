# frozen_string_literal: true

# What kind of product this chip ends up in (#190).
#
# The download step says something different to someone building an FPV VTX,
# someone deploying a hundred CCTV cameras and someone reflashing one doorbell,
# and the site had no way to tell them apart. Additive and nullable, so a
# rollback leaves the column behind and nothing reads it -- Soc#segment_name
# treats a null as `unknown`, which is the generic copy.
#
# The classification is written out here rather than called on the model. It
# used to be Soc.classify_segments!, and Soc is not a table any more (#289) --
# data/catalogue/*.yml carries each chip's segment -- so a migration that
# named the model would stop running on any database still behind this one.
class AddSegmentToSocs < ActiveRecord::Migration[8.1]
  def up
    add_column :socs, :segment, :string
    execute("UPDATE socs SET segment = 'fpv' WHERE segment IS NULL " \
            "AND LOWER(model) IN ('ssc338q', 'ssc30kq', 'ssc377qe', 'ssc378qe')")
    execute("UPDATE socs SET segment = 'consumer' WHERE segment IS NULL " \
            "AND vendor_id IN (SELECT id FROM vendors WHERE name = 'Ingenic')")
    execute("UPDATE socs SET segment = 'cctv' WHERE segment IS NULL " \
            "AND vendor_id IN (SELECT id FROM vendors WHERE name IN ('HiSilicon', 'Goke'))")
  end

  def down
    remove_column :socs, :segment
  end
end
