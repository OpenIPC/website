class Vendor < ApplicationRecord
  has_many :socs
  validates :name, uniqueness: true

  def self.find(id)
    find_by('lower(name) = ?', id) || find_by_id(id)
  end

  def to_param
    urlname
  end

  def urlname
    name.downcase
  end
end
