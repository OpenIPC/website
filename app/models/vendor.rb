class Vendor < ApplicationRecord
  has_many :socs
  validates :name, uniqueness: true
end
