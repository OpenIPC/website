# frozen_string_literal: true

# A chip maker, as data/catalogue/<urlname>.yml describes it (#289). Not a
# table any more: see Catalogue.
class Vendor
  include ActiveModel::Model
  include ActiveModel::Attributes

  ATTRIBUTES = %w[name urlname full_name website_url notes].freeze

  attribute :name, :string
  attribute :urlname, :string
  attribute :full_name, :string
  attribute :website_url, :string
  attribute :notes, :string

  # A urlname is an address and a filename (#162, #161): it is the path segment
  # under /cameras/vendors/, the name of a prerendered directory, and the name
  # of a file under data/catalogue. Derived from the name when a file does not
  # give one, which only downcases and replaces spaces -- so a name with a
  # slash or a dot in it produced a slug that walks out of every one of those.
  # Refused here, where all three read it, rather than sanitised at each.
  URLNAME_FORMAT = /\A[a-z0-9][a-z0-9._-]*\z/

  validates :name, presence: true
  validates :urlname, presence: true, format: { with: URLNAME_FORMAT, message: 'is not a safe slug' }

  def initialize(attributes = {})
    super
    self.urlname = name.to_s.downcase.gsub(' ', '-') if urlname.blank?
  end

  def socs
    @socs ||= []
  end

  # Only the vendors with a chip in the catalogue. Every vendor file lists at
  # least one today; the scope outlived the sensor makers who shared the old
  # table, and keeps the home page's vendor strip honest if a file ever lists
  # none.
  def self.soc_vendors
    all.select { |vendor| vendor.socs.any? }
  end

  def self.all
    Catalogue.current.vendors.sort_by(&:name)
  end

  def self.count
    Catalogue.current.vendors.size
  end

  # Rails hands `find` whatever came out of the URL, and `to_param` returns the
  # slug. Raises rather than returning nil, because that is what every caller
  # assumes: `find(params[:id])` followed by a method call on the result.
  # RescueHandler already turns RecordNotFound into a 404 page.
  #
  # Numeric ids no longer resolve. They were a database's, and the catalogue
  # has none; the addresses they appeared in have been slugs since #154.
  def self.find(id)
    find_by_param(id) ||
      raise(ActiveRecord::RecordNotFound, "Couldn't find #{name} with urlname #{id.inspect}")
  end

  # The nil-returning half, for callers where the identifier is an optional
  # filter rather than the thing being addressed.
  def self.find_by_param(id)
    return nil if id.blank?

    Catalogue.current.vendor(id)
  end

  def to_param
    urlname
  end

  def ==(other)
    other.is_a?(Vendor) && other.urlname == urlname
  end
  alias eql? ==

  def hash
    urlname.hash
  end
end
