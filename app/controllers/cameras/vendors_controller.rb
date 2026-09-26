# frozen_string_literal: true

module Cameras
  class VendorsController < ApplicationController
    def index
      @vendors = Vendor.all
      @socs = Soc.all
      @page_title = 'Full list of processors'
      render 'cameras/socs/index'
    end

    def show
      @vendor = Vendor.find(params[:id])
      @socs = @vendor.socs.sort_by(&:model)
      @page_title = "List of #{@vendor.name} SoCs"
      render 'cameras/socs/index'
    end
  end
end
