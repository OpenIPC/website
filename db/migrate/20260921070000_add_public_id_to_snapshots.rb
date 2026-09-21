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
# NULLABLE, DELIBERATELY. The deploy rule is that a rollback restores the
# image and never the schema, so this column has to be harmless to an image
# that knows nothing about it. A NOT NULL with no default is not: the previous
# release, restored, inserts a snapshot without the column and the database
# refuses it -- every camera upload failing until someone notices. The same
# window exists forwards, because deploy.sh leaves the old container serving
# while migrations run. The unique index is what matters here and it tolerates
# nulls; the NOT NULL can follow in its own migration once no image that omits
# the column can come back.
#
# Backfilled in Ruby rather than with one UPDATE ... SHA2(), so that the value
# comes from the same rule new rows use. A hex string is all digits about once
# in ten thousand, and both this app and the nginx vhost read an all-digit
# snapshot address as the retired numeric form -- a row backfilled with one
# would answer 410 for its whole life. Generating it here rather than calling
# Snapshot.generate_public_id keeps the migration from depending on a model
# that will keep changing.
class AddPublicIdToSnapshots < ActiveRecord::Migration[8.1]
  def up
    add_column :snapshots, :public_id, :string, limit: 20
    say_with_time('backfilling public_id') { backfill }
    add_index :snapshots, :public_id, unique: true
  end

  def down
    remove_index :snapshots, :public_id
    remove_column :snapshots, :public_id
  end

  private

  def backfill
    taken = Set.new
    ids = select_values('SELECT id FROM snapshots WHERE public_id IS NULL')
    ids.each do |id|
      execute("UPDATE snapshots SET public_id = #{quote(unique_token(taken))} WHERE id = #{id.to_i}")
    end
    ids.size
  end

  # Twenty lowercase hex characters, never all digits. Hex and not base64
  # because this database is utf8mb4_general_ci, where a unique index folds
  # case, so a mixed-case token could collide with its own spelling.
  def unique_token(taken)
    loop do
      candidate = SecureRandom.hex(10)
      next if candidate.match?(/\A[0-9]+\z/)

      return candidate if taken.add?(candidate)
    end
  end
end
