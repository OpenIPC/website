# frozen_string_literal: true

require 'yaml'

# The hardware catalogue, read from data/catalogue/*.yml (#289).
#
# Since #161 those files have been the reviewed copy of the catalogue and the
# prerendered pages are built from them. The `socs` and `vendors` tables were a
# second copy, reconciled by `rake catalogue:seed` and checked by
# `rake catalogue:diff`, which nothing ran -- a drift generator with no alarm
# on it. Now there is one copy, and it is this.
#
# Loaded once per process and never written: the catalogue changes in a pull
# request, which ships a new image, so there is nothing to invalidate between
# deploys. 126 SoCs across 14 files is about a millisecond to parse.
#
# Every record is validated on load and a bad one raises, naming the file. A
# slug is an address and a filename (#162), so a malformed or duplicated one is
# refused at boot (config/initializers/catalogue.rb) rather than discovered as
# a 404 or a file written somewhere it should not be.
class Catalogue
  DIR = Rails.root.join('data/catalogue').to_s

  class Invalid < StandardError; end

  class << self
    def current
      @current ||= load
    end

    # For tests, which start each case from a catalogue of their own.
    attr_writer :current

    def load(dir = DIR)
      new(Dir[File.join(dir, '*.yml')].sort.map { |file| [file, YAML.safe_load_file(file)] })
    end
  end

  attr_reader :vendors

  def initialize(documents = [])
    @vendors = []
    documents.each { |file, data| add_document(file, data) }
  end

  # Every SoC, in the order the lists show them: by vendor, then by model.
  def socs
    vendors.sort_by(&:name).flat_map { |vendor| vendor.socs.sort_by(&:model) }
  end

  def vendor(urlname)
    vendors.find { |vendor| vendor.urlname == urlname.to_s }
  end

  def soc(urlname)
    vendors.each do |vendor|
      found = vendor.socs.find { |soc| soc.urlname == urlname.to_s }
      return found if found
    end
    nil
  end

  def add_vendor(vendor)
    check!(vendor, 'vendor')
    raise Invalid, "two vendors are called #{vendor.urlname}" if vendor(vendor.urlname)

    @vendors << vendor
    vendor
  end

  def add_soc(soc)
    check!(soc, 'SoC')
    raise Invalid, "two SoCs are called #{soc.urlname}" if soc(soc.urlname)
    raise Invalid, "#{soc.vendor.name} lists #{soc.model} twice" if listed?(soc)

    soc.vendor.socs << soc
    soc
  end

  private

  def add_document(file, data)
    socs = data.fetch('socs', [])
    vendor = add_vendor(Vendor.new(data.slice(*Vendor::ATTRIBUTES)))
    socs.each { |attributes| add_soc(Soc.new(attributes.slice(*Soc::ATTRIBUTES).merge('vendor' => vendor))) }
  rescue Invalid, KeyError => e
    raise Invalid, "#{File.basename(file)}: #{e.message}"
  end

  def listed?(soc)
    soc.vendor.socs.any? { |other| other.model == soc.model }
  end

  def check!(record, kind)
    return if record.valid?

    raise Invalid, "#{kind} #{record.urlname.inspect}: #{record.errors.full_messages.join(', ')}"
  end
end
