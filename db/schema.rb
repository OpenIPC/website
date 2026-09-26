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

ActiveRecord::Schema[8.1].define(version: 2026_09_26_070000) do
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

  create_table "firmware_builds", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "ip_address", limit: 45, null: false
    t.index ["created_at"], name: "index_firmware_builds_on_created_at"
    t.index ["ip_address", "created_at"], name: "index_firmware_builds_on_ip_address_and_created_at"
  end

  create_table "snapshots", charset: "utf8mb4", collation: "utf8mb4_general_ci", force: :cascade do |t|
    t.string "caption"
    t.datetime "created_at", null: false
    t.string "firmware"
    t.string "flash_size"
    t.string "hostname"
    t.string "ip_address"
    t.string "mac_address"
    t.string "public_id", limit: 20
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
    t.index ["public_id"], name: "index_snapshots_on_public_id", unique: true
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
    t.string "segment"
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

  add_foreign_key "active_storage_attachments", "active_storage_blobs", column: "blob_id"
  add_foreign_key "active_storage_variant_records", "active_storage_blobs", column: "blob_id"
end
