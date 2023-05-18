# frozen_string_literal: true

module Cameras
  class VendorsController < ApplicationController
    def index
      @vendors = Vendor.order(:name)
      @socs = Soc.joins(:vendor).order(:name, :model)
      @page_title = "Full list of processors"
      render "cameras/socs/index"
    end

    def show
      @vendor = Vendor.find(params[:id])
      @socs = @vendor.socs.order(:model)
      @page_title = "List of #{@vendor.name} SoCs"
      render "cameras/socs/index"
    end
  end
end
