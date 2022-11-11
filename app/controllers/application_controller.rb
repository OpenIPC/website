class ApplicationController < ActionController::Base
  include RubyMineHacks if Rails.env.development?
  include RescueHandler

  protect_from_forgery unless: -> { request.format.json? }

  add_flash_types :alert, :notice, :danger, :info, :success, :warning

  def route_not_found
    File.open(Rails.root.join('public/notfound.txt'), 'a') do |f|
      f.puts "#{request.original_url} from #{request.referer}"
    end
    #ApplicationMailer.with(request: request).missing_page.deliver
    redirect_to '/'
  end
end
