module Cameras
  class VendorsController < ApplicationController
    def index
      @vendors = Vendor.order(:name)
      @socs = Soc.joins(:vendor).order(:name, :model)
      @page_title = "Cameras: Full list"
      render "cameras/socs/index"
    end

    def show
      @vendor = Vendor.find(params[:id])
      @socs = @vendor.socs.order(:model)
      @page_title = "Cameras: Filtered by #{@vendor.name}"
      render "cameras/socs/index"
    end
  end
end
