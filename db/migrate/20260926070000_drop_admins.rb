# frozen_string_literal: true

# The admin is gone (#288), and this was its only table: six Devise accounts,
# their email addresses and their password digests. Keeping them after the code
# that used them only keeps credentials nobody can use and somebody could leak.
#
# Under the deploy rule that a rollback restores the image and never the
# schema, the previous release comes back to a database without this table.
# That release boots and serves every public page -- Devise names the model at
# route-drawing time but reads nothing from the table until someone signs in --
# so what a rollback loses is the admin, which is what this release removed.
#
# IF EXISTS because deploy/refresh-dev.sh drops the table from dev's copy of a
# production backup before this has run there. The block is the table as
# schema.rb last described it, so `db:rollback` recreates it -- empty.
class DropAdmins < ActiveRecord::Migration[8.1]
  DATETIMES = %i[confirmation_sent_at confirmed_at current_sign_in_at last_sign_in_at locked_at
                 remember_created_at reset_password_sent_at].freeze
  STRINGS = %i[confirmation_token current_sign_in_ip last_sign_in_ip reset_password_token
               unconfirmed_email unlock_token].freeze
  UNIQUE = %i[confirmation_token email reset_password_token unlock_token].freeze

  def change
    drop_table :admins, if_exists: true do |t|
      DATETIMES.each { |column| t.datetime column }
      STRINGS.each { |column| t.string column }
      t.string :email, default: '', null: false
      t.string :encrypted_password, default: '', null: false
      t.integer :failed_attempts, default: 0, null: false
      t.integer :sign_in_count, default: 0, null: false
      t.timestamps
      UNIQUE.each { |column| t.index column, unique: true }
    end
  end
end
