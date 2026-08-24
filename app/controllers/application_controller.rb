# frozen_string_literal: true

class ApplicationController < ActionController::Base
  include RubyMineHacks if Rails.env.development?
  include Multilang
  include RescueHandler

  protect_from_forgery unless: -> { request.format.json? }

  add_flash_types :alert, :notice, :danger, :info, :success, :warning

  # Logged, not written to public/notfound.txt as this used to be. That file
  # sat in the directory the app serves, so every unmatched URL and the referer
  # that produced it were readable by anyone: https://openipc.org/notfound.txt
  # answered 200. A referer carries wherever the visitor came from, which is
  # not ours to publish, and the accumulated list is a map of what people probe
  # -- 966 lines inside three hours of one deploy. It was also unbounded within
  # a deploy cycle, and had no reader: nothing in the app or the ops scripts
  # ever opened it.
  def route_not_found
    Rails.logger.info(
      "[route_not_found] #{request.original_url} from #{request.referer.presence || '-'}"
    )
    redirect_to '/'
  end
end
