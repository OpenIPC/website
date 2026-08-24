# frozen_string_literal: true

class ApplicationController < ActionController::Base
  include RubyMineHacks if Rails.env.development?
  include Multilang
  include RescueHandler

  protect_from_forgery unless: -> { request.format.json? }

  add_flash_types :alert, :notice, :danger, :info, :success, :warning

  # This used to append every unmatched URL, and the referer that produced it,
  # to public/notfound.txt -- a file in the directory the app serves, so
  # https://openipc.org/notfound.txt answered 200 to anyone who asked. A
  # referer says where a visitor came from, which is not ours to publish, and
  # the accumulated list is a map of what people probe. It reached 966 lines
  # inside three hours of one deploy, and nothing ever read it.
  #
  # Nothing replaces it here, because nothing needs to: nginx's combined log
  # already records the path, the status and the referer for every request, in
  # a rotated file on the host rather than a world-readable one in public/.
  # Writing it again would duplicate that at info level for every miss.
  def route_not_found
    redirect_to '/'
  end
end
