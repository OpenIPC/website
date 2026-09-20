# frozen_string_literal: true

class AdminController < ApplicationController
  # Never publicly cacheable. Every page behind this controller is rendered for
  # one signed-in person and several show uploader IP and MAC addresses;
  # Rails' own `max-age=0, private` is the right answer (#155). The
  # admin_signed_in? guard upstream would catch these anyway -- this is the
  # belt to its braces, and it holds even for a route reached before Devise
  # has established the session.
  def publicly_cacheable?
    false
  end

  before_action :authenticate_admin!
  # before_action :disable_xss_protection
end
