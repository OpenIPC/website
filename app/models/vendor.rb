# frozen_string_literal: true

class Vendor < ApplicationRecord
  has_many :socs
  has_many :sensors

  before_validation :generate_urlname

  # A urlname is an address and a filename (#162, #161): it is the path segment
  # under /cameras/vendors/, the name of a prerendered directory, and the name
  # of a file under data/catalogue. `generate_urlname` only downcases and
  # replaces spaces, so a name with a slash or a dot in it -- which an
  # administrator can enter -- produced a slug that walks out of every one of
  # those. Refused here, where all three read it, rather than sanitised at each.
  URLNAME_FORMAT = /\A[a-z0-9][a-z0-9._-]*\z/

  validates :name, presence: true, uniqueness: true
  validates :urlname, presence: true, uniqueness: true,
                      format: { with: URLNAME_FORMAT, message: 'is not a safe slug' }

  # distinct is load-bearing, not tidiness: the join produces one row per SoC,
  # so without it the scope yields HiSilicon twenty-three times and the
  # homepage's vendor strip prints every name once per chip.
  scope :soc_vendors, -> { left_joins(:socs).where.not(socs: { id: nil }).distinct }

  # Rails hands `find` whatever came out of the URL, and `to_param` returns the
  # slug, so a slug has to resolve first; ids still work, for old links and for
  # the admin forms that pass one.
  #
  # This raises rather than returning nil, because that is what every caller
  # already assumes: `#{model}.find(params[:id])` followed by a method call on
  # the result. Returning nil turned an unknown slug into a NoMethodError on
  # nil deep inside the request -- /cameras/vendors/ingenic/socs/t31 answered
  # 500 where t31x answered with firmware, because there is no SoC called
  # plain "t31". RescueHandler already turns RecordNotFound into a 404 page.
  def self.find(id)
    find_by_param(id) ||
      raise(ActiveRecord::RecordNotFound,
            "Couldn't find #{name} with urlname or id #{id.inspect}")
  end

  # The nil-returning half, for callers where the identifier is an optional
  # filter rather than the thing being addressed.
  def self.find_by_param(id)
    return nil if id.blank?

    find_by(urlname: id) || find_by(id: id)
  end

  def to_param
    urlname
  end

  private

  def generate_urlname
    self.urlname = name.downcase.gsub(' ', '-')
  end
end
