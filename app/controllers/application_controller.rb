class ApplicationController < ActionController::Base
  include RubyMineHacks if Rails.env.development?
  include RescueHandler
end
