# frozen_string_literal: true

require 'test_helper'

# db/schema.rb stays the dump the app's own Rails produces (#236).
#
# The file was committed in 7.0 format while the app ran 8.1, and the two
# versions dump the same database differently -- 8.1 orders columns
# alphabetically within a table, 7.0 kept definition order -- so the first
# `db:migrate` anybody ran reordered 130 lines across every table. One added
# column arrived as a 65-line diff.
#
# That is not data drift and it never was: the column set was identical either
# way. What it costs is review. A schema diff is the one place in this
# repository where somebody is reading for which column moved and what a
# rollback will not undo, because deploy/DEV-VALIDATION.md and CLAUDE.md both
# warn that rollback restores the image and never the schema -- so migrations
# have to stay additive, and this file is where that gets checked.
#
# The dump was regenerated in ae5ce17 and a migration now costs two changed
# lines: the version bump and the column. These assertions are what keep it
# there, because the way it comes back is quiet -- somebody runs `db:migrate`
# under a Rails that is not the one Gemfile.lock pins, and commits the
# reformat along with their real change.
class SchemaDumpTest < ActiveSupport::TestCase
  SCHEMA = Rails.root.join('db/schema.rb')

  # The major.minor Active Record actually resolved to at boot. Read from the
  # loaded gem rather than parsed out of Gemfile.lock: it is the version whose
  # dumper would write this file, which is the thing being compared.
  def running_version
    ActiveRecord::VERSION::STRING.split('.').first(2).join('.')
  end

  def declared_version
    SCHEMA.read[/ActiveRecord::Schema\[([\d.]+)\]/, 1]
  end

  def declared_version_stamp
    SCHEMA.read[/define\(version: ([\d_]+)\)/, 1].to_s.delete('_')
  end

  test 'the dump is in the format the running Rails writes' do
    assert declared_version, 'db/schema.rb does not declare a schema version at all'

    assert_equal running_version, declared_version, <<~MESSAGE.chomp
      db/schema.rb says ActiveRecord::Schema[#{declared_version}] and this app
      runs Active Record #{ActiveRecord::VERSION::STRING}. The next `db:migrate`
      will rewrite the whole file to the running format, and that reordering
      will land inside somebody's unrelated change -- which is how it got here.

      Regenerate it on its own:

        docker compose run --rm web bin/rails db:drop db:create db:schema:load db:migrate
    MESSAGE
  end

  SETTING = 'config.active_record.schema_format = :ruby'

  # Uncommented lines only. The runtime value cannot tell the explicit setting
  # from Rails' own :ruby default, so this raw-text check is the whole of the
  # guard -- and a substring search over the file would be satisfied by the
  # commented-out line, which is the one state it exists to reject. It would
  # also be satisfied by the paragraph above the setting, which quotes it.
  def sets_schema_format?(source)
    source.lines.any? { |line| !line.strip.start_with?('#') && line.include?(SETTING) }
  end

  # A structure.sql switch would be a legitimate decision and is not one to
  # make by accident -- config/application.rb says why it is :ruby, with the
  # round-trip that backs it. This fails if the setting goes away, so the
  # reasoning cannot be silently dropped along with it.
  test 'the schema format is a decision, not a default' do
    assert_equal :ruby, Rails.application.config.active_record.schema_format

    assert sets_schema_format?(Rails.root.join('config/application.rb').read), <<~MESSAGE.chomp
      config/application.rb no longer sets the schema format. :ruby is also the
      Rails default, so leaving it unwritten makes the next person's `structure.sql`
      question look unanswered when it has been answered -- see #236.
    MESSAGE
  end

  # The guard above has exactly one way to fail quietly: commenting the line
  # out leaves the runtime value at :ruby by default and leaves the text in the
  # file. Same matcher, so this cannot drift away from what actually runs.
  test 'a commented-out setting does not count as setting it' do
    assert_not sets_schema_format?("    # #{SETTING}\n"),
               'a commented-out assignment satisfies the schema-format guard'
    assert_not sets_schema_format?("# see also `#{SETTING}` below\n"),
               'prose quoting the setting satisfies the schema-format guard'
    assert sets_schema_format?("    #{SETTING}\n"),
           'the matcher does not recognise the setting when it is actually set'
  end

  # A schema dumped at a migration whose file never got committed. The file
  # then stops describing what `db:schema:load` builds on a fresh host, and
  # deploy/RESTORE.md builds fresh hosts.
  #
  # Only this direction. A schema BEHIND its migrations is already refused by
  # Rails, which declines to run the suite at all -- checked, rather than
  # assumed, by putting the version back and watching `bin/rails test` answer
  # "Migrations are pending" and run nothing. Ahead is the quiet one.
  test 'the dump is not ahead of the migrations that produced it' do
    newest = Rails.root.glob('db/migrate/*.rb')
                  .map { |f| f.basename.to_s[/\A(\d+)/, 1] }
                  .compact.max

    assert newest, 'no migrations found; this test is reading nothing'
    assert_equal newest, declared_version_stamp, <<~MESSAGE.chomp
      db/schema.rb is at version #{declared_version_stamp} and the newest
      migration committed is #{newest}, so the migration this dump was taken
      from is not in the repository. A fresh database built with
      `db:schema:load` is then not the database this code expects
    MESSAGE
  end
end
