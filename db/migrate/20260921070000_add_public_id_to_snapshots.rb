# frozen_string_literal: true

# A snapshot's public identifier, so the Open Wall cannot be walked.
#
# The ids are sequential, and on 2026-09-21 a fleet of roughly a thousand
# residential-proxy addresses was fetching /snapshots/<id> in order, one
# request per id, against a robots.txt that has always said Disallow. Three
# requests find the end of the range. An address ban cannot catch a thousand
# rotating addresses making twenty-five requests each, and the fingerprint
# that does identify them -- a User-Agent claiming macOS beside a 1,366 px
# viewport -- only arrives with the beacon, after the page has been served.
#
# So the capability goes instead of the actor: an identifier that cannot be
# derived from another one stops this fleet, the next one, and anyone who
# writes a for-loop, with no list to keep current.
#
# Additive, per the deploy rule: a rollback restores the image and not the
# schema, so the column has to be harmless to code that does not know about
# it. Nothing reads it until the model does.
#
# Lowercase hex rather than base64: this database is utf8mb4_general_ci, where
# a unique index folds case, so a mixed-case token would collide with its own
# spelling and lookups would match the wrong row.
class AddPublicIdToSnapshots < ActiveRecord::Migration[8.1]
  def up
    add_column :snapshots, :public_id, :string, limit: 20

    # Backfill in one statement rather than row by row. The wall holds a few
    # thousand rows at the two-day retention, and SHA2 of the id with a random
    # per-run salt is unguessable without being a Ruby loop over the table.
    salt = SecureRandom.hex(16)
    execute(<<~SQL.squish)
      UPDATE snapshots
         SET public_id = LEFT(SHA2(CONCAT(#{connection.quote(salt)}, id), 256), 20)
       WHERE public_id IS NULL
    SQL

    change_column_null :snapshots, :public_id, false
    add_index :snapshots, :public_id, unique: true
  end

  def down
    remove_index :snapshots, :public_id
    remove_column :snapshots, :public_id
  end
end
