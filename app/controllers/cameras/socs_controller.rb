module Cameras
  class SocsController < ApplicationController
    # include InstallationInstructionConcern

    def index
      @vendor = Vendor.find(params[:vendor])
      if @vendor
        @socs = Soc.left_joins(:vendor).where(vendors: { name: @vendor }).order(:model)
        @page_title = "SoC: filtered by #{@vendor}"
      else
        @socs = Soc.left_joins(:vendor).order(:name, :model)
        @page_title = "SoC: full list"
      end
      render "cameras/socs/index"
    end

    def show
      @camera = Camera.new(
        camera_ip_address: "192.168.1.10",
        server_ip_address: "192.168.1.254",
        flash_type: "nor8m",
        network_interface: "eth",
        sd_card_slot: "nosd"
      )
      @camera.camera_ip_address  = params[:cip] if params[:cip]
      @camera.server_ip_address  = params[:sip] if params[:sip]
      @camera.flash_type         = params[:rom] if params[:rom]
      @camera.network_interface  = params[:net] if params[:net]
      @camera.sd_card_slot       = params[:sd]  if params[:sd]

      @soc = Soc.find_by_urlname(params[:id])
      @vendor = @soc.vendor

      @page_title = "SoC: #{@soc.full_name}"
      render "cameras/socs/show"
    end

    def legend
      render "cameras/socs/legend"
    end

    def update
      @camera = Camera.new(
        camera_ip_address: "192.168.1.10",
        server_ip_address: "192.168.1.254",
        flash_type: "nor8m",
        network_interface: "eth",
        sd_card_slot: "nosd"
      )
      @camera.camera_ip_address  = permitted_params[:camera_ip_address]
      @camera.server_ip_address  = permitted_params[:server_ip_address]
      @camera.flash_type         = permitted_params[:flash_type]
      @camera.network_interface  = permitted_params[:network_interface]
      @camera.sd_card_slot       = permitted_params[:sd_card_slot]

      # to handle nor32m size still using nor16m command
      @flash_type_command = @camera.flash_type
      @flash_type_command = "nor16m" if @camera.flash_type.eql?("nor32m")

      @soc = Soc.find(params[:id])
      @backup_filename = "backup-#{@soc.model.downcase}-#{@camera.flash_type}.bin"

      @page_title = "SoC: #{@soc.full_name}"
      render "cameras/socs/update"
    end

    private

      def permitted_params
        params.require(:camera).permit(:flash_type, :sd_card_slot, :network_interface, :camera_ip_address, :server_ip_address)
      end
  end
end
