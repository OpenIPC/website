module Cameras
  class SocsController < ApplicationController
    def index
      @_vendor = params[:vendor] if params[:vendor]
      if @_vendor.in?(Vendor.all.map(&:name))
        @socs = Soc.left_joins(:vendor).where(vendors: { name: @_vendor }).order(:model)
        @page_title = "Supported SoCs: filtered by #{@_vendor}"
      else
        @socs = Soc.left_joins(:vendor).order(:name, :model)
        @page_title = "Supported SoCs: full List"
      end
      render "cameras/socs/index"
    end

    def show
      @soc = Soc.find(params[:id])
      @page_title = "SoC #{@soc.full_name}"
      render "cameras/socs/show"
    end

    def update
      @soc = Soc.find(params[:id])
      @ipaddr = permitted_params[:ipaddr]
      @serverip = permitted_params[:serverip]
      @flash_type = permitted_params[:flash_type]
      @network_ifaces = permitted_params[:network_ifaces]
      @backup_filename = "backup-#{@soc.model.downcase}-#{@flash_type}.bin"
      if @flash_type.eql?("nor16m")
        @flash_size_hex = "0x1000000" # 16M
      else
        @flash_size_hex = "0x800000" # 8M
      end
      @page_title = "SoC #{@soc.full_name}"
      render "cameras/socs/update", status: :unprocessable_entity
    end

    private

      def permitted_params
        params.require(:camera).permit(:ipaddr, :serverip, :flash_size, :flash_type, :network_ifaces)
      end
  end
end
