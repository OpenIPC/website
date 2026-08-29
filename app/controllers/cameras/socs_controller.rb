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
      apply_permalink_to(@camera)

      # Soc.find, like every other action. find_by_urlname answers nil for a
      # slug that does not exist, and the next line then raises NoMethodError on
      # nil -- so /cameras/vendors/rockchip/socs/rv1106, a SoC this site has no
      # row for, is a 500 rather than a 404. That is the exact failure Soc.find
      # was overridden to prevent, and this action was the one place still
      # bypassing it. RescueHandler turns RecordNotFound into the 404 page.
      @camera.soc = Soc.find(params[:id])
      @vendor = @camera.soc.vendor

      narrow_to_what_the_menu_offers(@camera)

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
      @camera.partition_layout = permitted_params[:partition_layout]
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

      # The bootloader macros are named after the layout, not the chip. This
      # used to be the flash type with `nor32m` rewritten to `nor16m`, which is
      # the same answer for every combination the menu could then produce --
      # there is no mtdpartsnor32m anywhere upstream, so a 32MB part has always
      # worn the 16MB layout. Camera#partition_layout says it directly now, and
      # says it for the 8MB-layout-on-a-larger-chip case too.
      #
      # After the flash type has settled, not before: the layout defaults to
      # the chip's own, so reading it first left the page telling a 16MB camera
      # to `run urnor16m` and then erasing from the 8MB overlay offset, 733,184
      # bytes into what it had just written. That is the failure #60 described,
      # by another route.
      warn_if_layout_changed permitted_params[:partition_layout]
      @flash_type_command = @camera.partition_layout

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

        warn_if_nothing_published

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
      permitted_params = params.permit(:id, :vendor_id, :flash_size, :fw_release, :flash_type, :layout)
      flash_size = permitted_params[:flash_size]
      flash_type = permitted_params[:flash_type]
      fw_release = permitted_params[:fw_release]
      @soc = Soc.find(params[:id])
      # An absent layout means the chip's own, which is what every link written
      # before the wizard could tell the two apart meant.
      fw = Firmware.new(size: flash_size, flash_type: flash_type, release: fw_release, soc: @soc,
                        layout: permitted_params[:layout])
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

    # Read a configuration back out of the query string Camera#permalink writes.
    #
    # The keys are the permanent link's, and they have to stay in step with it:
    # `permalink` emitted `var` for as long as it existed against a `ver` that
    # was never written, so the edition was the one field the link dropped and
    # an Ultimate link reopened as Lite. `permalink` now writes `ver`; `var`
    # stays readable here because every link anyone has shared carries it, and
    # `ver` wins if a link somehow has both.
    # One table rather than seven near-identical lines, because the drift was
    # between a key and a field and a table is where that is visible. Insertion
    # order is the precedence: `var` is applied first so `ver` overwrites it.
    PERMALINK_FIELDS = { cip: :camera_ip_address, sip: :server_ip_address, rom: :flash_type,
                         part: :partition_layout,
                         var: :firmware_version, ver: :firmware_version,
                         net: :network_interface, sd: :sd_card_slot }.freeze

    def apply_permalink_to(camera)
      # Not in the table: unlike the rest, the MAC is rewritten rather than
      # copied, and it is applied whether or not the link carried one.
      camera.camera_mac_address = params[:mac].to_s.downcase.gsub('-', ':')

      # present?, not just presence of the key. A permanent link carries every
      # field whether or not it has a value, so `?...&ver=&sd=` is what a link
      # built from a camera with no edition chosen looks like -- and the menu
      # can produce exactly that, since allowedEditions falls back to '' when a
      # chip has nothing published for it. Treating the empty string as an
      # answer blanked the dropdown; blank means "not specified", so the
      # constructor default stands.
      PERMALINK_FIELDS.each do |key, field|
        camera.public_send("#{field}=", params[key]) if params[key].present?
      end
    end

    # Bring a configuration that arrived in the query string back inside what the
    # menu on this page actually offers. Both rules below are the menu's; it
    # applies them in JavaScript, after this action has already decided which
    # options open selected.
    #
    # Silent, unlike the equivalents in `update`. There the choice decides what
    # gets flashed and the visitor is told when it changes; here it only decides
    # where a form opens, and they are about to press the button anyway.
    def narrow_to_what_the_menu_offers(camera)
      # nor8m is the default for almost every SoC and wrong for the ones upstream
      # builds only a NAND image for -- rv1109 and rv1126 here today. Their NOR
      # sizes are disabled in the menu, so the form opened on a disabled flash
      # type with no edition to go with it. Unrecognised is the same as unset:
      # ?rom=nor64m otherwise opened the form on a chip that matches no option.
      camera.flash_type = camera.soc.default_flash_chip unless camera.flash_type.in?(Camera::FLASH_CHIP)

      # Ultimate does not fit an 8MB chip. Reading `var` back made this
      # reachable: ?rom=nor8m&var=ultimate opened the form on a combination
      # enforce_eight_meg_limit refuses. Same carve-out as that method -- a SoC
      # published as Ultimate and nothing else, hi3516cv6xx and hi3519dv500,
      # keeps it, because naming a Lite tarball upstream never built is worse
      # than the size warning `update` will give.
      return unless camera.partition_layout.eql?('nor8m') && camera.firmware_version.eql?('ultimate')
      return unless camera.soc.available_releases('nor').include?('lite')

      camera.firmware_version = 'lite'
    end

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
      return unless eight_meg_rootfs_with_ultimate?

      published = @camera.soc.available_releases('nor')
      # Nothing on NOR at any size is a different problem, and
      # warn_if_nothing_published has already named it. "Needs a larger chip"
      # would be wrong advice here: no NOR size helps a NAND-only part.
      return if published.empty?

      if published.include?('lite')
        @camera.firmware_version = 'lite'
        flash.now[:warning] = eight_meg_warning
      else
        flash.now[:alert] = no_lite_for_eight_meg_alert
      end
    end

    # The layout, not the chip: the 8MB one gives the rootfs 5120KB wherever it
    # is written, and that is what Ultimate does not fit in.
    def eight_meg_rootfs_with_ultimate?
      @camera.partition_layout.eql?('nor8m') && @camera.firmware_version.eql?('ultimate')
    end

    # "This SoC needs a larger chip" is the right advice for an 8MB part and the
    # wrong advice for a 16MB one wearing the 8MB layout, where the chip is
    # already big enough and the layout is the thing to change. The guard above
    # reaches both since it became the layout's, so this has to tell them apart
    # too -- it is the branch for a SoC published as Ultimate and nothing else,
    # hi3516cv6xx and hi3519dv500, where there is no Lite to fall back to.
    def no_lite_for_eight_meg_alert
      unless @camera.flash_type.eql?('nor8m')
        return 'The Ultimate edition does not fit the 8MB partition layout, and OpenIPC publishes ' \
               'no Lite build for this SoC on NOR. Choose the 16MB layout, which this chip is big ' \
               'enough for.'
      end

      'The Ultimate edition does not fit an 8MB flash chip, and OpenIPC publishes no Lite build ' \
        'for this SoC on NOR. These instructions cannot produce a working camera on 8MB flash -- ' \
        'this SoC needs a larger chip.'
    end

    # The chip when the chip is what limits them, and the layout when it is the
    # layout: a 5120KB rootfs partition is a 5120KB rootfs partition whether the
    # part around it is 8MB or 32MB, and on the larger ones there is something
    # the reader can actually do about it.
    def eight_meg_warning
      return '8MB Flash ROM can only be flashed with Lite or FPV edition!' if @camera.flash_type.eql?('nor8m')

      'The 8MB partition layout leaves 5MB for the rootfs, which only the Lite and FPV editions ' \
        'fit. Choose the 16MB layout to install Ultimate on this chip.'
    end

    # Nothing published for the chip that was chosen, at any edition.
    #
    # Distinct from use_published_release!, which moves the visitor onto a
    # published edition and deliberately says nothing when there is no edition
    # to move them to -- `return nil if available.empty?`. That silence was
    # invisible while update forced every submission onto default_flash_chip,
    # which picks a flash type that has builds. Honouring the submitted chip
    # made it reachable: NAND on any of the ~110 SoCs with no NAND build, or
    # NOR on a NAND-only part like rv1109, rendered `run uknand; run urnand`,
    # a bundle link and a download link, all naming a tarball upstream never
    # built and all 404ing off-site with nothing here to explain why.
    #
    # available_releases answers the known list rather than [] when the index
    # cannot be read, so an unreachable index does not turn into "OpenIPC
    # publishes nothing for this SoC".
    # The layout is refused rather than clamped in silence when the chip cannot
    # hold it -- reachable only from a hand-edited query string, since the menu
    # does not offer the 16MB layout on an 8MB part, but it decides where the
    # rootfs is written and a page that quietly showed the other one would be
    # describing a different install from the one that was asked for.
    def warn_if_layout_changed(asked)
      # Unrecognised is the same as unset, as it is everywhere else here, and
      # NAND has one layout and no menu to choose it from.
      return if @camera.nand? || !asked.in?(Camera::PARTITION_LAYOUT)
      return if asked.eql?(@camera.partition_layout)

      flash.now[:warning] =
        'The 16MB partition layout needs a 16MB chip -- its rootfs alone ends at 0xD50000. ' \
        "Showing the #{@camera.layout_size}MB layout instead."
    end

    def warn_if_nothing_published
      return if @camera.soc.available_releases(@camera.flash_type_type).any?

      flash.now[:alert] =
        "OpenIPC publishes no #{@camera.flash_type_type.upcase} firmware for this SoC. These " \
        'instructions cannot produce a working camera -- the files they name have never been built.'
    end

    def permitted_params
      params.require(:camera).permit(
        :flash_type, :partition_layout, :sd_card_slot, :network_interface, :camera_ip_address,
        :server_ip_address, :firmware_version, :sd_card_slot, :camera_mac_address
      )
    end
  end
end
