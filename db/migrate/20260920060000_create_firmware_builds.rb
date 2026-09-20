# frozen_string_literal: true

# One row per firmware image actually assembled, with the address that asked
# for it. It exists to answer one question -- how many builds has this address
# caused in the last minute (#147) -- and rows are deleted within the hour.
#
# A table rather than a cache store, because there is no cache store here and
# adding one is a decision the migration plan took the other way: the box is
# memory-bound and a :memory_store would be per-worker, which is not a shared
# counter at all. The volume this has to carry is ~113 rows a day.
class CreateFirmwareBuilds < ActiveRecord::Migration[7.0]
  def change
    # Pinned to match every other table here. MariaDB 11.8 defaults utf8mb4 to
    # uca1400_ai_ci, so a create_table that inherits the server default gives
    # this one table a different collation from the rest of the schema
    # depending on which MariaDB built it.
    create_table :firmware_builds, charset: 'utf8mb4', collation: 'utf8mb4_general_ci' do |t|
      t.string :ip_address, null: false, limit: 45 # room for a full IPv6 literal
      t.datetime :created_at, null: false
    end

    # The only query: count rows for one address inside a window. Address
    # first, so the index answers it without touching a row.
    add_index :firmware_builds, %i[ip_address created_at]
    # Pruning walks by age alone.
    add_index :firmware_builds, :created_at
  end
end
