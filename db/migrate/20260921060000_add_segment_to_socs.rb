# frozen_string_literal: true

# What kind of product this chip ends up in (#190).
#
# The download step says something different to someone building an FPV VTX,
# someone deploying a hundred CCTV cameras and someone reflashing one doorbell,
# and the site had no way to tell them apart. Additive and nullable, so a
# rollback leaves the column behind and nothing reads it -- Soc#segment_name
# treats a null as `unknown`, which is the generic copy.
#
# Raw SQL rather than the model: a migration that calls Soc.update_all is a
# migration that breaks the day someone renames a scope, and this one has to
# keep running on a fresh database years from now.
class AddSegmentToSocs < ActiveRecord::Migration[8.1]
  def up
    add_column :socs, :segment, :string

    Soc::SEGMENT_SEED.each { |segment, models| assign_by_model(segment, models) }
    assign_by_vendor('consumer', ['Ingenic'])
    assign_by_vendor('cctv', %w[HiSilicon Goke])
  end

  def down
    remove_column :socs, :segment
  end

  private

  def assign_by_model(segment, models)
    return if models.empty?

    list = models.map { |m| connection.quote(m) }.join(', ')
    execute(<<~SQL.squish)
      UPDATE socs SET segment = #{connection.quote(segment)} WHERE LOWER(model) IN (#{list})
    SQL
  end

  # Only where nothing more specific has been set, so the FPV parts above keep
  # the segment they were just given rather than being swept back into cctv.
  def assign_by_vendor(segment, vendors)
    list = vendors.map { |v| connection.quote(v) }.join(', ')
    execute(<<~SQL.squish)
      UPDATE socs SET segment = #{connection.quote(segment)}
      WHERE segment IS NULL AND vendor_id IN (SELECT id FROM vendors WHERE name IN (#{list}))
    SQL
  end
end
