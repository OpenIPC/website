# frozen_string_literal: true

class AdminController < ApplicationController
  before_action :authenticate_admin!
  # before_action :disable_xss_protection
end
