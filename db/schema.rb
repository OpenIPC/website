# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[7.0].define(version: 2022_08_06_130659) do
  create_table "active_storage_attachments", charset: "utf8mb4", force: :cascade do |t|
    t.string "name", null: false
    t.string "record_type", null: false
    t.bigint "record_id", null: false
    t.bigint "blob_id", null: false
    t.datetime "created_at", null: false
    t.index ["blob_id"], name: "index_active_storage_attachments_on_blob_id"
    t.index ["record_type", "record_id", "name", "blob_id"], name: "index_active_storage_attachments_uniqueness", unique: true
  end

  create_table "active_storage_blobs", charset: "utf8mb4", force: :cascade do |t|
    t.string "key", null: false
    t.string "filename", null: false
    t.string "content_type"
    t.text "metadata"
    t.string "service_name", null: false
    t.bigint "byte_size", null: false
    t.string "checksum"
    t.datetime "created_at", null: false
    t.index ["key"], name: "index_active_storage_blobs_on_key", unique: true
  end

  create_table "active_storage_variant_records", charset: "utf8mb4", force: :cascade do |t|
    t.bigint "blob_id", null: false
    t.string "variation_digest", null: false
    t.index ["blob_id", "variation_digest"], name: "index_active_storage_variant_records_uniqueness", unique: true
  end

  create_table "admins", charset: "utf8mb4", force: :cascade do |t|
    t.string "email", default: "", null: false
    t.string "encrypted_password", default: "", null: false
    t.string "reset_password_token"
    t.datetime "reset_password_sent_at"
    t.datetime "remember_created_at"
    t.integer "sign_in_count", default: 0, null: false
    t.datetime "current_sign_in_at"
    t.datetime "last_sign_in_at"
    t.string "current_sign_in_ip"
    t.string "last_sign_in_ip"
    t.string "confirmation_token"
    t.datetime "confirmed_at"
    t.datetime "confirmation_sent_at"
    t.string "unconfirmed_email"
    t.integer "failed_attempts", default: 0, null: false
    t.string "unlock_token"
    t.datetime "locked_at"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["confirmation_token"], name: "index_admins_on_confirmation_token", unique: true
    t.index ["email"], name: "index_admins_on_email", unique: true
    t.index ["reset_password_token"], name: "index_admins_on_reset_password_token", unique: true
    t.index ["unlock_token"], name: "index_admins_on_unlock_token", unique: true
  end

  create_table "custom_commands", charset: "utf8mb4", force: :cascade do |t|
    t.bigint "soc_id"
    t.string "command_block"
    t.text "text"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["soc_id"], name: "index_custom_commands_on_soc_id"
  end

  create_table "sensors", charset: "utf8mb4", force: :cascade do |t|
    t.string "urlname"
    t.bigint "vendor_id"
    t.string "model"
    t.string "mode"
    t.string "optical_format"
    t.string "imager_size"
    t.string "active_pixels"
    t.string "pixel_size"
    t.string "color_filter_array"
    t.string "max_data_rate"
    t.string "max_fps_full"
    t.string "max_fps_vga"
    t.string "adc_resolution"
    t.string "responsivity"
    t.string "pixel_dynamic_range"
    t.string "snr_max"
    t.string "voltage"
    t.string "power_consumption"
    t.string "operating_temp"
    t.string "packaging"
    t.string "status"
    t.text "notes"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["urlname"], name: "index_sensors_on_urlname", unique: true
    t.index ["vendor_id"], name: "index_sensors_on_vendor_id"
  end

  create_table "snapshots", charset: "utf8mb4", force: :cascade do |t|
    t.string "mac_address"
    t.string "ip_address"
    t.string "hostname"
    t.string "soc"
    t.string "sensor"
    t.string "flash_size"
    t.string "firmware"
    t.string "uptime"
    t.string "soc_temperature"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
  end

  create_table "socs", charset: "utf8mb4", force: :cascade do |t|
    t.bigint "vendor_id"
    t.string "family"
    t.string "model"
    t.string "version"
    t.string "status"
    t.string "load_address"
    t.string "sdk"
    t.string "kernel"
    t.string "uboot_filename"
    t.string "linux_filename"
    t.text "notes"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.string "urlname"
    t.string "toolchain_filename"
    t.string "build_status_url"
    t.boolean "featured", default: false, null: false
    t.index ["urlname"], name: "index_socs_on_urlname", unique: true
    t.index ["vendor_id"], name: "index_socs_on_vendor_id"
  end

  create_table "vendors", charset: "utf8mb4", force: :cascade do |t|
    t.string "name"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.string "urlname"
    t.string "full_name"
    t.string "website_url"
    t.text "notes"
    t.index ["urlname"], name: "index_vendors_on_urlname", unique: true
  end

  add_foreign_key "active_storage_attachments", "active_storage_blobs", column: "blob_id"
  add_foreign_key "active_storage_variant_records", "active_storage_blobs", column: "blob_id"
end
