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

      # Soc.find, like every other action. find_by_urlname answers nil for a
      # slug that does not exist, and the next line then raises NoMethodError on
      # nil -- so /cameras/vendors/rockchip/socs/rv1106, a SoC this site has no
      # row for, is a 500 rather than a 404. That is the exact failure Soc.find
      # was overridden to prevent, and this action was the one place still
      # bypassing it. RescueHandler turns RecordNotFound into the 404 page.
      @camera.soc = Soc.find(params[:id])
      @vendor = @camera.soc.vendor

      # nor8m is the default for almost every SoC and wrong for the ones
      # upstream builds only a NAND image for -- rv1109 and rv1126 here today.
      # Their NOR sizes are disabled in the menu, so the form opened on a
      # disabled flash type with no edition to go with it. Unrecognised is the
      # same as unset: ?rom=nor64m otherwise opened the form on a chip that
      # matches no option in it.
      @camera.flash_type = @camera.soc.default_flash_chip unless @camera.flash_type.in?(Camera::FLASH_CHIP)

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

      @camera.soc = Soc.find(params[:id])
      @vendor = @camera.soc.vendor

      # nor8m is the default for almost every SoC and wrong for the ones
      # upstream builds only a NAND image for -- rv1109 and rv1126 here today.
      # Their NOR sizes are disabled in the menu, so the form opened on a
      # disabled flash type with no edition to go with it.
      #
      # `show` guards this on params[:rom] because a query string is where its
      # choice arrives. This action is reached by PUT from the form, which sends
      # camera[flash_type] and never sends rom -- so the same guard here was
      # always true and threw away every choice the visitor made.
      #
      # Against FLASH_CHIP rather than for blankness, because a chip this site
      # does not know is not a choice either. Camera#flash_size_hex and friends
      # fall through to their 8MB branch for anything unrecognised, while
      # @flash_type_command below would go on to render `run setnor64m` and a
      # printenv hint naming three variables no bootloader defines. Nothing
      # calls valid? on a Camera, so this is the only thing standing between the
      # query string and the commands.
      @camera.flash_type = @camera.soc.default_flash_chip unless @camera.flash_type.in?(Camera::FLASH_CHIP)

      # to handle nor32m size still using nor16m command. After the default
      # above, not before: the commands name the chip, so reading the flash type
      # first left the page telling a 16MB camera to `run urnor16m` and then
      # erasing from the 8MB overlay offset, 733,184 bytes into what it had just
      # written. That is the failure #60 described, by another route.
      @flash_type_command = @camera.flash_type
      @flash_type_command = 'nor16m' if @camera.flash_type.eql?('nor32m')

      if @vendor.name.eql?("SigmaStar") && @camera.flash_type.eql?("nand")
        render 'cameras/socs/sigmastar_nand_is_weird'
      elsif @camera.soc.model.in?(%w[HI3536CV100 HI3536DV100])
        render 'cameras/socs/hi3536dv100_is_weird'
      else
        # Everything above this point can be set from the query string.
        if (asked = @camera.use_published_release!)
          flash.now[:warning] =
            "OpenIPC does not publish a #{asked.to_s.capitalize} build for this SoC on " \
            "#{@camera.flash_type_type.upcase} flash. Showing #{@camera.firmware_version_name} instead."
        end

        # After use_published_release!, not before. The size rule has to be
        # applied to the edition that is actually going to be rendered, and this
        # is the call that settles it: on a SoC published as Ultimate and nothing
        # else, a `lite` submission arrives here as Lite, leaves as Ultimate, and
        # the size rule had already looked and seen Lite. nor8m + Ultimate then
        # rendered with no mention of it.
        #
        # The same read-before-it-settled mistake the flash type had above.
        enforce_eight_meg_limit

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

    # Ultimate does not fit an 8MB chip: its rootfs is larger than the 5120k
    # `rootfs` partition mtdpartsnor8m defines, so `run urnor8m` cannot write it.
    #
    # Downgrade to Lite when there is a Lite build to downgrade to. Doing it
    # unconditionally would be wrong -- hi3516cv6xx and hi3519dv500 are
    # published as Ultimate and nothing else, and naming a Lite tarball upstream
    # never built swaps a size problem the visitor can see for a missing file
    # they cannot.
    #
    # When there is nothing to fall back to, say so rather than going quiet. The
    # page otherwise rendered a full set of 8MB Ultimate instructions -- a
    # download link for `flash_size=8&fw_release=ultimate`, and `run uknor8m;
    # run urnor8m` -- with no indication that none of it can work.
    def enforce_eight_meg_limit
      return unless @camera.flash_type.eql?('nor8m') && @camera.firmware_version.eql?('ultimate')

      published = @camera.soc.available_releases('nor')

      if published.include?('lite')
        @camera.firmware_version = 'lite'
        flash.now[:warning] = '8MB Flash ROM can only be flashed with Lite or FPV edition!'
      elsif published.empty?
        # No NOR firmware at all, at any size -- rv1109 and rv1126 are like this.
        # Saying "needs a larger chip" here would send the visitor to buy one
        # that will not help either.
        flash.now[:alert] =
          'OpenIPC publishes no NOR firmware for this SoC at all. These instructions cannot produce ' \
          'a working camera; this SoC is supported on NAND flash only.'
      else
        flash.now[:alert] =
          'The Ultimate edition does not fit an 8MB flash chip, and OpenIPC publishes no Lite build ' \
          'for this SoC on NOR. These instructions cannot produce a working camera on 8MB flash -- ' \
          'this SoC needs a larger chip.'
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
