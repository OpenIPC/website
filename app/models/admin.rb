class Admin < ApplicationRecord
  # :registerable, :confirmable and :omniauthable
  devise :database_authenticatable, :lockable, :recoverable, :rememberable, :timeoutable, :trackable, :validatable
end
