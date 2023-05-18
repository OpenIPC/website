# frozen_string_literal: true

class ApplicationRecord < ActiveRecord::Base
  primary_abstract_class
  after_validation :log_errors, if: Proc.new { |m| m.errors }

  def log_errors
    Rails.logger.debug self.errors.full_messages.join("\n")
  end
end
