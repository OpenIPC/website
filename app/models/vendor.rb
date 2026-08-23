# frozen_string_literal: true

class Vendor < ApplicationRecord
  has_many :socs
  has_many :sensors

  before_validation :generate_urlname

  validates :name, presence: true, uniqueness: true
  validates :urlname, presence: true, uniqueness: true

  scope :soc_vendors, -> { left_joins(:socs).where.not(socs: { id: nil }) }

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
