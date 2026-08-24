# frozen_string_literal: true

module Cameras
  class SocsController < ApplicationController
    # include InstallationInstructionConcern

    def index
      respond_to do |format|
        format.html {
          # ?vendor= is the only thing this action does. Without it there is no
          # list to render -- the template calls `render @socs` unconditionally,
          # so leaving it unset answered 500 -- and /supported-hardware already
          # treats "no filter" as meaning the featured page.
          if params[:vendor].blank?
            redirect_to '/supported-hardware/featured'
          else
            # Present but unknown is a bad address, not an empty list.
            @vendor = Vendor.find(params[:vendor])
            @socs = @vendor.socs.order(:model)
            @page_title = "List of #{@vendor.name} SoCs"
            render 'cameras/socs/index'
          end
        }
        format.json do
          @data = {
            vendors: Vendor.order(:name).map do |v|
              {
                name: v.name,
                socs: v.socs.order(:model).map do |s|
                  {
                    family: s.family,
                    model: s.model,
                    version: s.version,
                    uboot: s.uboot_filename,
                    kernel: s.kernel,
                    rootfs: s.linux_filename,
                    sdk: s.sdk,
                    load_address: s.load_address,
                    status: s.status,
                  }
                end
              }
            end
          }
          render json: @data #.to_json
        end
      end
    end

    def show
      @camera = Camera.new(
        camera_ip_address: '192.168.1.10',
        server_ip_address: '192.168.1.254',
        flash_type: 'nor8m',
        firmware_version: 'lite',
        network_interface: 'eth',
        sd_card_slot: 'nosd'
      )
      @camera.camera_ip_address = params[:cip] if params[:cip]
      @camera.camera_mac_address = params[:mac].to_s.downcase.gsub('-', ':')
      @camera.server_ip_address = params[:sip] if params[:sip]
      @camera.flash_type = params[:rom] if params[:rom]
      @camera.firmware_version = params[:ver] if params[:ver]
      @camera.network_interface = params[:net] if params[:net]
      @camera.sd_card_slot = params[:sd] if params[:sd]

      @camera.soc = Soc.find_by_urlname(params[:id])
      @vendor = @camera.soc.vendor

      @page_title = "SoC: #{@camera.soc.full_name}"
      render 'cameras/socs/show'
    end

    def update
      @camera = Camera.new(
        camera_ip_address: '192.168.1.10',
        server_ip_address: '192.168.1.254',
        flash_type: 'nor8m',
        firmware_version: 'lite',
        network_interface: 'eth',
        sd_card_slot: 'nosd'
      )
      @camera.camera_ip_address = permitted_params[:camera_ip_address]
      @camera.camera_mac_address = permitted_params[:camera_mac_address].to_s.downcase.gsub('-', ':')
      @camera.server_ip_address = permitted_params[:server_ip_address]
      @camera.flash_type = permitted_params[:flash_type]
      @camera.firmware_version = permitted_params[:firmware_version]
      @camera.network_interface = permitted_params[:network_interface]
      @camera.sd_card_slot = permitted_params[:sd_card_slot]

      # to handle nor32m size still using nor16m command
      @flash_type_command = @camera.flash_type
      @flash_type_command = 'nor16m' if @camera.flash_type.eql?('nor32m')

      @camera.soc = Soc.find(params[:id])
      @vendor = @camera.soc.vendor

      if @vendor.name.eql?("SigmaStar") && @camera.flash_type.eql?("nand")
        render 'cameras/socs/sigmastar_nand_is_weird'
      elsif @camera.soc.model.in?(%w[HI3536CV100 HI3536DV100])
        render 'cameras/socs/hi3536dv100_is_weird'
      else
        if @camera.flash_type.eql?('nor8m') && @camera.firmware_version.eql?('ultimate')
          @camera.firmware_version = 'lite'
          flash.now[:warning] = '8MB Flash ROM can only be flashed with Lite or FPV edition!'
        end

        @camera.backup_filename = "backup-#{@camera.soc.model.downcase}-#{@camera.flash_type}.bin"

        @page_title = "SoC: #{@camera.soc.full_name}"
        render 'cameras/socs/update'
      end
    end

    def download_full_image
      permitted_params = params.permit(:id, :vendor_id, :flash_size, :fw_release, :flash_type)
      flash_size = permitted_params[:flash_size]
      flash_type = permitted_params[:flash_type]
      fw_release = permitted_params[:fw_release]
      @soc = Soc.find(params[:id])
      fw = Firmware.new(size: flash_size, flash_type: flash_type, release: fw_release, soc: @soc)
      fw.generate
      # Recorded here rather than in Firmware, because a cached image is sent
      # without being rebuilt and it is the sending that is worth counting.
      Download.record(firmware: fw, soc: @soc, bytes: File.size(fw.filepath))
      send_file fw.filepath, filename: fw.filename, disposition: :attachment
    rescue Firmware::PayloadTooLarge => e
      # The combination is real but does not fit -- Ultimate on 8MB flash is
      # the one users actually hit. The wizard downgrades it to Lite and says
      # so, but this action takes flash_size and fw_release from the query
      # string, so say the same thing here rather than claiming the firmware
      # does not exist.
      Rails.logger.warn "full image does not fit for #{params[:id]}: #{e.message}"
      flash.alert = 'This edition is too large for that flash size. Try the Lite edition, or a larger flash.'
      redirect_back(fallback_location: '/')
    rescue ActionController::MissingFile, Firmware::MissingMember, Firmware::InvalidFlashSize => e
      # MissingMember means the release tarball does not carry what this flash
      # type needs -- a NAND build with no kernel member, say. Better a missing
      # download than an image with a hole where the rootfs should be.
      # InvalidFlashSize means flash_size was not one this generator builds for;
      # it arrives straight from the query string, so it is refused before it
      # can be turned into an allocation.
      Rails.logger.warn "full image unavailable for #{params[:id]}: #{e.message}"
      flash.alert = 'This firmware does not exist.'
      redirect_back(fallback_location: '/')
    rescue ReleaseCache::UnknownAsset => e
      # Upstream does not publish this asset. The name came from the SoC's
      # uboot_filename or linux_filename, so this is a record naming a build
      # that does not exist rather than anything the visitor did.
      #
      # Which of the two is missing is worth saying. For every Xiongmai SoC and
      # the whole GK7102 family it is the bootloader, permanently -- firmware
      # for them is published and linked on the SoC page -- and "this firmware
      # does not exist" sends that visitor looking for a fault that is not
      # there.
      Rails.logger.warn "full image has no upstream asset for #{params[:id]}: #{e.message}"
      flash.alert = missing_asset_message(@soc)
      redirect_back(fallback_location: '/')
    rescue ReleaseCache::Unavailable => e
      # It exists but cannot be had right now -- GitHub is unreachable, or the
      # bytes did not match what the index promised. Saying so is better than
      # claiming it does not exist, because retrying is the right advice.
      Rails.logger.error "full image unavailable for #{params[:id]}: #{e.message}"
      flash.alert = 'This firmware could not be fetched right now. Please try again in a few minutes.'
      redirect_back(fallback_location: '/')
    rescue Firmware::LockTimeout => e
      # Another request has been building this image for a minute. Saying so is
      # better than reporting it missing, because retrying will work.
      Rails.logger.error "full image lock timeout for #{params[:id]}: #{e.message}"
      flash.alert = 'This firmware is being prepared right now. Please try again in a moment.'
      redirect_back(fallback_location: '/')
    end

    def featured
      @socs = Soc.left_joins(:vendor).where(featured: true).order(:name, :model)
      @page_title = 'List of recommended SoCs'
      render 'cameras/socs/index'
    end

    def full_list
      @socs = Soc.left_joins(:vendor).order(:name, :model)
      @page_title = 'SoC: full list'
      render 'cameras/socs/index'
    end

    private

    # Which half is missing, in the visitor's terms.
    #
    # "This firmware does not exist" was the answer to all three, and for two
    # of them it is both untrue and unactionable: firmware for the SoC exists,
    # is published, and is linked on the page they came from.
    def missing_asset_message(soc)
      return 'This firmware does not exist.' if soc.nil?

      if !soc.firmware_published?
        'OpenIPC does not publish firmware for this SoC yet.'
      elsif !soc.bootloader_published?
        'OpenIPC does not publish a bootloader for this SoC, so a full flash image cannot be ' \
          'assembled for it. The firmware bundle on the SoC page is published and can be ' \
          'installed with the bootloader your camera already has.'
      else
        # Both halves exist; this edition or flash type is the part that does not.
        'This firmware does not exist.'
      end
    end

    def permitted_params
      params.require(:camera).permit(
        :flash_type, :sd_card_slot, :network_interface, :camera_ip_address,
        :server_ip_address, :firmware_version, :sd_card_slot, :camera_mac_address
      )
    end
  end
end
