# frozen_string_literal: true

# ugly hack for RubyMine IDE
module RubyMineHacks
  extend ActiveSupport::Concern

  included do
    #
  end

  # @return [Admin]
  def current_admin
    super
  end
end
