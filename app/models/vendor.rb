class Vendor < ApplicationRecord
  has_many :socs

  before_validation :generate_urlname

  validates :name, uniqueness: true
  validates :urlname, uniqueness: true
  
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
