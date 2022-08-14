class CustomCommand < ApplicationRecord
  belongs_to :soc

  validates :soc, presence: true
  validates :command_block, uniqueness: { scope: :soc_id }
end
