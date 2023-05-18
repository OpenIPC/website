# frozen_string_literal: true

class Admin < ApplicationRecord
  devise :database_authenticatable, :lockable, :recoverable, :rememberable, :timeoutable, :trackable, :validatable
end
