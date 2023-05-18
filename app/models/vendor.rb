# frozen_string_literal: true

class Vendor < ApplicationRecord
  has_many :socs
  has_many :sensors

  before_validation :generate_urlname

  validates :name, presence: true, uniqueness: true
  validates :urlname, presence: true, uniqueness: true

  scope :soc_vendors, -> { left_joins(:socs).where.not(socs: { id: nil }) }

  def self.find(id)
    find_by_urlname(id) || find_by_id(id)
  end

  def to_param
    urlname
  end

  private

    def generate_urlname
      self.urlname = name.downcase.gsub(' ', '-')
    end
end
