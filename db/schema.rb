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

ActiveRecord::Schema[8.1].define(version: 2026_09_19_190000) do
  create_table "active_storage_attachments", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.bigint "blob_id", null: false
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.bigint "record_id", null: false
    t.string "record_type", null: false
    t.index ["blob_id"], name: "index_active_storage_attachments_on_blob_id"
    t.index ["record_type", "record_id", "name", "blob_id"], name: "index_active_storage_attachments_uniqueness", unique: true
  end

  create_table "active_storage_blobs", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.bigint "byte_size", null: false
    t.string "checksum"
    t.string "content_type"
    t.datetime "created_at", null: false
    t.string "filename", null: false
    t.string "key", null: false
    t.text "metadata"
    t.string "service_name", null: false
    t.index ["key"], name: "index_active_storage_blobs_on_key", unique: true
  end

  create_table "active_storage_variant_records", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.bigint "blob_id", null: false
    t.string "variation_digest", null: false
    t.index ["blob_id", "variation_digest"], name: "index_active_storage_variant_records_uniqueness", unique: true
  end

  create_table "admins", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.datetime "confirmation_sent_at"
    t.string "confirmation_token"
    t.datetime "confirmed_at"
    t.datetime "created_at", null: false
    t.datetime "current_sign_in_at"
    t.string "current_sign_in_ip"
    t.string "email", default: "", null: false
    t.string "encrypted_password", default: "", null: false
    t.integer "failed_attempts", default: 0, null: false
    t.datetime "last_sign_in_at"
    t.string "last_sign_in_ip"
    t.datetime "locked_at"
    t.datetime "remember_created_at"
    t.datetime "reset_password_sent_at"
    t.string "reset_password_token"
    t.integer "sign_in_count", default: 0, null: false
    t.string "unconfirmed_email"
    t.string "unlock_token"
    t.datetime "updated_at", null: false
    t.index ["confirmation_token"], name: "index_admins_on_confirmation_token", unique: true
    t.index ["email"], name: "index_admins_on_email", unique: true
    t.index ["reset_password_token"], name: "index_admins_on_reset_password_token", unique: true
    t.index ["unlock_token"], name: "index_admins_on_unlock_token", unique: true
  end

  create_table "downloads", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.integer "bytes"
    t.datetime "created_at", null: false
    t.integer "flash_size"
    t.string "flash_type", null: false
    t.string "release", null: false
    t.bigint "soc_id"
    t.string "soc_model", null: false
    t.index ["created_at"], name: "index_downloads_on_created_at"
    t.index ["soc_id"], name: "index_downloads_on_soc_id"
    t.index ["soc_model", "created_at"], name: "index_downloads_on_soc_model_and_created_at"
  end

  create_table "sensors", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.string "active_pixels"
    t.string "adc_resolution"
    t.string "color_filter_array"
    t.datetime "created_at", null: false
    t.string "imager_size"
    t.string "max_data_rate"
    t.string "max_fps_full"
    t.string "max_fps_vga"
    t.string "mode"
    t.string "model"
    t.text "notes"
    t.string "operating_temp"
    t.string "optical_format"
    t.string "packaging"
    t.string "pixel_dynamic_range"
    t.string "pixel_size"
    t.string "power_consumption"
    t.string "responsivity"
    t.string "snr_max"
    t.string "status"
    t.datetime "updated_at", null: false
    t.string "urlname"
    t.bigint "vendor_id"
    t.string "voltage"
    t.index ["urlname"], name: "index_sensors_on_urlname", unique: true
    t.index ["vendor_id"], name: "index_sensors_on_vendor_id"
  end

  create_table "snapshots", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.string "caption"
    t.datetime "created_at", null: false
    t.string "firmware"
    t.string "flash_size"
    t.string "hostname"
    t.string "ip_address"
    t.string "mac_address"
    t.string "sensor"
    t.string "soc"
    t.string "soc_temperature"
    t.string "streamer"
    t.datetime "updated_at", null: false
    t.string "uptime"
    t.datetime "variants_generated_at"
    t.index ["created_at"], name: "index_snapshots_on_created_at"
    t.index ["flash_size"], name: "index_snapshots_on_flash_size"
    t.index ["ip_address"], name: "index_snapshots_on_ip_address"
    t.index ["mac_address"], name: "index_snapshots_on_mac_address"
    t.index ["sensor"], name: "index_snapshots_on_sensor"
    t.index ["soc"], name: "index_snapshots_on_soc"
  end

  create_table "socs", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.string "build_status_url"
    t.datetime "created_at", null: false
    t.string "family"
    t.boolean "featured", default: false, null: false
    t.string "kernel"
    t.string "linux_filename"
    t.string "load_address"
    t.string "model"
    t.text "notes"
    t.string "sdk"
    t.string "status"
    t.string "uboot_filename"
    t.datetime "updated_at", null: false
    t.string "urlname"
    t.bigint "vendor_id"
    t.string "version"
    t.index ["urlname"], name: "index_socs_on_urlname", unique: true
    t.index ["vendor_id"], name: "index_socs_on_vendor_id"
  end

  create_table "vendors", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "full_name"
    t.string "name"
    t.text "notes"
    t.datetime "updated_at", null: false
    t.string "urlname"
    t.string "website_url"
    t.index ["urlname"], name: "index_vendors_on_urlname", unique: true
  end

  add_foreign_key "active_storage_variant_records", "active_storage_blobs", column: "blob_id"
end
