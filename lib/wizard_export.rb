# frozen_string_literal: true

require 'json'

# The wizard's output, as data (#163).
#
# The installation wizard is ~850 lines of flash geometry -- Camera,
# FlashLayout and InstallationHelper -- whose output tells somebody what to
# paste into a bootloader with a camera's only copy of its firmware at stake.
# `InstallationHelper#guarded_flash` chains transfer, erase and write with `&&`
# and bounds the write with ${filesize}; the comments around it cite three
# field reports where a missing step bricked a camera.
#
# So none of it is ported. It is enumerated: for every combination the menu can
# offer, Rails renders its own command lines here, and both halves of the site
# read the result. There is one producer, and the suite that already covers the
# wizard covers the export by construction.
#
# The three per-visitor values are holes rather than data. They appear only
# inside `setenv ipaddr`, `setenv serverip`, `setenv ethaddr` and in the backup
# filename, so the export carries {{ipaddr}}, {{serverip}} and {{ethaddr}} and
# whoever renders it fills them. Nothing about where the rootfs is written
# crosses into JavaScript.
#
# Warnings are keys, not sentences (see the same issue's step 3): a key and its
# arguments can be rendered in the visitor's language at the other end.
module WizardExport
  IPADDR = '{{ipaddr}}'
  SERVERIP = '{{serverip}}'
  # The MAC appears in two forms and they are not interchangeable: with colons
  # inside `setenv ethaddr`, and stripped inside the backup filename. One hole
  # for both would have a renderer writing `backup-...-aa:bb:cc:dd:ee:ff.bin`,
  # which is a different file from the one the page told them to make.
  ETHADDR = '{{ethaddr}}'
  ETHADDR_PLAIN = '{{ethaddr_plain}}'

  # A well-formed address, used only to find out which lines change when one is
  # given; it is substituted back out before anything is written.
  SAMPLE_MAC = 'aa:bb:cc:dd:ee:ff'

  # The blocks that carry commands. Each is a helper that returns lines; the
  # page wraps them in terminal chrome and the static wizard will do the same.
  BLOCKS = %w[
    firmware_backup flashing_everything flashing_uboot flashing_linux
    preparing_environment post_flash_environment restore_from_backup
  ].freeze

  # Where the export is written. Not into the bundle: it is a function of the
  # release index, which a publisher refreshes on the host, and the bundle is
  # built in CI where that file does not exist. So the pages fetch it, as they
  # fetch the backer count and the availability states, and nginx serves it
  # from the same shared directory.
  OUT_DIR = ENV.fetch('WIZARD_EXPORT_DIR', '/srv/www/shared/wizard')

  class << self
    # One SoC, every combination its menu can offer.
    #
    # The command blocks are pooled rather than repeated. Ninety combinations
    # produce 630 blocks and 82 distinct ones: the network interface changes
    # two lines of one block and nothing in the other six, and the SD slot
    # rather less. Written out in full the file was 400 KB, which is a lot to
    # hand a browser to render one page of it; pooled it is a fraction of that
    # and exactly the same data -- a combination names the block it uses and
    # the pool holds each distinct one once.
    def document(soc)
      @pool = {}
      @pool_ids = {}
      @variant_pool = {}
      @variant_ids = {}

      combinations = combinations(soc)

      {
        'soc' => soc.urlname,
        'model' => soc.model,
        'vendor' => soc.vendor.urlname,
        'load_address' => soc.load_address,
        # The board a firmware is built for, which is not always the model:
        # Ingenic ships one build per family. The bundle filenames the page
        # links to are built from it.
        'board' => soc.board,
        # What the SoC page decides between before it shows a form at all --
        # whether upstream publishes a bootloader and firmware for this chip.
        # Read from the release index, so it is a runtime answer like the
        # editions above and cannot be baked into the page.
        'instructable' => soc.instructable?,
        'availability' => soc.availability.to_s,
        'bootloader_published' => soc.bootloader_published?,
        'uboot_filename' => soc.uboot_filename,
        'linux_filename' => soc.linux_filename,
        'bl_url' => soc.bl_url,
        # One entry per bundle upstream actually publishes, for the links the
        # page offers when it cannot offer instructions. Built here rather
        # than from a filename template for the reason the view says: a link
        # this site cannot honour reads as our download being broken.
        'published' => soc.published_availability.flat_map do |flash_type, releases|
          releases.map do |release|
            {
              'flash_type' => flash_type,
              'release' => release,
              'url' => soc.fw_url(release, flash_type),
              'filename' => soc.linux_filename_for(release, flash_type),
            }
          end
        end,
        # Step 4: the patterns the form validates with, exported once so the
        # static form cannot grow a third copy.
        #
        # The browser's and the server's are different expressions of the same
        # rule and cannot be one string -- `IP_ADDRESS_FORMAT` is Resolv's, in
        # extended mode, and accepts IPv6, while an HTML `pattern` is a
        # JavaScript regex with no flags. Both are here, and
        # test/wizard_export_test.rb asserts the browser's is the stricter of
        # the two: a form that accepted what the server refuses would fail on
        # submit with nothing said.
        'patterns' => {
          'mac' => view.macaddr_pattern,
          'ip' => view.ipaddr_pattern,
        },
        'editions' => Soc::FLASH_TYPES.to_h { |type| [type, soc.available_releases(type)] },
        # What the edition menu lists before the page narrows it -- the union
        # across flash types, because the flash type is chosen in the same form
        # without a round trip. And where the form opens: a SoC upstream builds
        # only a NAND image for has its NOR sizes disabled, so nor8m would be a
        # choice that cannot be chosen.
        'offerable' => soc.offerable_releases,
        'default_flash_chip' => soc.default_flash_chip,
        # Which flash types get a page of their own rather than commands, by
        # the same rule `update` applies. Said here rather than left to the
        # renderer to rediscover, because rediscovering it is how the two
        # halves come to disagree about a camera somebody is about to flash.
        'special_pages' => Camera::FLASH_CHIP.filter_map do |flash_type|
          page = special_page(soc, flash_type)
          [flash_type, page] if page
        end.to_h,
        # Each distinct block once, named by the order it was first seen.
        'blocks' => @pool,
        'mac_variants' => @variant_pool,
        'combinations' => combinations,
      }
    end

    # The id this content already has, or the next one.
    def pool(store, ids, value)
      key = value.to_json
      ids[key] ||= begin
        id = ids.size.to_s
        store[id] = value
        id
      end
    end

    def json(soc)
      "#{JSON.pretty_generate(document(soc))}\n"
    end

    def path(soc, dir: OUT_DIR)
      File.join(dir, "#{soc.urlname}.json")
    end

    # Compact, because this one is fetched rather than read: pretty-printing it
    # adds a third for nobody's benefit, and gzip takes the compact form to
    # about 3 KB.
    def write_all(dir: OUT_DIR)
      FileUtils.mkdir_p(dir)

      written = Soc.includes(:vendor).find_each.map do |soc|
        document = document(soc)
        # Written and renamed, so a reader never sees half a file -- the same
        # arrangement oc-stats.sh uses for the backer count.
        temporary = "#{path(soc, dir: dir)}.tmp"
        File.write(temporary, "#{JSON.generate(document)}\n")
        FileUtils.chmod(0o644, temporary)
        FileUtils.mv(temporary, path(soc, dir: dir))
        [soc.urlname, document['combinations'].size]
      end

      # A SoC removed from the catalogue leaves a file that would go on being
      # served, describing a chip the site no longer lists.
      names = written.map { |(urlname, _)| "#{urlname}.json" }
      Dir[File.join(dir, '*.json')].each do |file|
        File.delete(file) unless names.include?(File.basename(file))
      end

      written
    end

    # Every combination the menu can reach for this SoC.
    #
    # Not the cross product: the form narrows what it offers, and a combination
    # nobody can select is a page nobody can reach. The partition layout only
    # exists on NOR, and `narrow_to_what_the_menu_offers`'s Ultimate-on-8MB
    # rule is applied here so the question of the two halves drifting does not
    # arise -- the export simply does not carry the combination the menu will
    # not offer.
    def combinations(soc)
      Camera::FLASH_CHIP.flat_map do |flash_type|
        layouts_for(flash_type).flat_map do |layout|
          editions_for(soc, flash_type, layout).flat_map do |edition|
            Camera::NET_IFACE.flat_map do |interface|
              Camera::SD_CARD.map do |sd|
                entry(soc, flash_type: flash_type, layout: layout, edition: edition,
                           interface: interface, sd: sd)
              end
            end
          end
        end
      end
    end

    private

    def layouts_for(flash_type)
      return [nil] if flash_type == 'nand'

      Camera::PARTITION_LAYOUT.select { |layout| layout_fits?(layout, flash_type) }
    end

    # A 16MB layout needs a 16MB chip. The menu does not offer it on an 8MB
    # part, and `warn_if_layout_changed` refuses it if a query string asks.
    def layout_fits?(layout, flash_type)
      return true if flash_type == 'nor8m' && layout == 'nor8m'

      layout == 'nor8m' || flash_type.in?(%w[nor16m nor32m])
    end

    # Which editions this flash type can be asked for.
    #
    # The published ones where upstream publishes any: `use_published_release!`
    # moves anything else onto the first of them, so no other edition can reach
    # a rendered page.
    #
    # Where it publishes none, that method returns early and the asked edition
    # is what gets rendered -- with `nothing_published` over it -- so every
    # edition the menu lists is reachable and each one needs a page here. This
    # is the tail a hand-edited query string reaches, and covering it is what
    # lets the static wizard answer from the export alone instead of guessing
    # when it finds nothing.
    def editions_for(soc, flash_type, layout)
      published = soc.available_releases(flash_type.start_with?('nor') ? 'nor' : 'nand')
      # Where this family publishes nothing, every edition this site knows is
      # reachable, and the union with the other family's covers a name upstream
      # has that this site does not. Narrowing it to `offerable_releases` was
      # wrong in both directions and showed on dev: SSC333DE publishes nothing
      # at all, so the list was the one-element fallback, and asking for
      # Ultimate found no page where Rails renders a full one.
      offered = published.presence || (Camera::FW_VERSION + soc.offerable_releases).uniq

      # The Ultimate-on-8MB rule, from narrow_to_what_the_menu_offers: dropped
      # only when there is a Lite to fall back to, because naming a tarball
      # upstream never built is worse than the size warning.
      if layout == 'nor8m' && published.include?('lite')
        offered - ['ultimate']
      else
        offered
      end
    end

    # Two combinations are not a wizard at all.
    #
    # `Cameras::SocsController#update` answers SigmaStar on NAND and the two
    # HI3536 NVRs with a page of their own -- the procedure is different enough
    # that showing the generated commands would be wrong rather than
    # incomplete. The export says which page rather than leaving the renderer
    # to rediscover the rule, because rediscovering it is how the two halves
    # come to disagree about a camera somebody is about to flash.
    def special_page(soc, flash_type)
      return 'sigmastar_nand' if soc.vendor.name == 'SigmaStar' && flash_type == 'nand'
      return 'hi3536dv100' if soc.model.in?(%w[HI3536CV100 HI3536DV100])

      nil
    end

    def entry(soc, flash_type:, layout:, edition:, interface:, sd:)
      if (page = special_page(soc, flash_type))
        return {
          'flash_type' => flash_type,
          'partition_layout' => layout,
          'edition' => edition,
          'network_interface' => interface,
          'sd_card_slot' => sd,
          'page' => page,
        }
      end

      camera = camera_for(soc, flash_type: flash_type, layout: layout, edition: edition,
                               interface: interface, sd: sd)

      {
        'flash_type' => flash_type,
        'partition_layout' => layout || camera.partition_layout,
        'edition' => edition,
        'network_interface' => interface,
        'sd_card_slot' => sd,
        'flash_size' => camera.flash_size,
        'layout_size' => camera.layout_size,
        # What the result page shows around the commands: which steps it has,
        # which bundle it links to, and which bootloader variables the hint at
        # the bottom names. Facts rather than markup -- the page is rendered at
        # the other end, in the visitor's language.
        'flash_family' => camera.flash_type_type,
        'firmware_url' => view.firmware_url(camera),
        'firmware_filename' => view.firmware_filename(camera),
        'default_bootloader_layout' => camera.default_bootloader_layout?,
        'layout_commands' => camera.layout_commands.any?,
        'bootloader_variables' => camera.bootloader_variables,
        'blocks' => blocks_for(camera),
        'mac_variant' => mac_variant_for(soc, flash_type: flash_type, layout: layout,
                                              edition: edition, interface: interface, sd: sd),
        'warnings' => warnings_for(soc, flash_type: flash_type, layout: layout, edition: edition),
      }
    end

    # What changes when the visitor gives a MAC address.
    #
    # Only the blocks that differ, so the file does not carry two copies of
    # everything: a camera with an address gets `setenv ethaddr` in its
    # environment and the address in its backup filename, and every other line
    # is identical.
    def mac_variant_for(soc, flash_type:, layout:, edition:, interface:, sd:)
      with_mac = camera_for(soc, flash_type: flash_type, layout: layout, edition: edition,
                                 interface: interface, sd: sd, mac: SAMPLE_MAC)
      without = camera_for(soc, flash_type: flash_type, layout: layout, edition: edition,
                                interface: interface, sd: sd)

      BLOCKS.each_with_object({}) do |block, differences|
        theirs = lines_of(with_mac, block)
                 .map { |line| line.gsub(SAMPLE_MAC, ETHADDR) }
                 .map { |line| line.gsub(SAMPLE_MAC.delete(':'), ETHADDR_PLAIN) }
        ours = lines_of(without, block)
        differences[block] = pool(@variant_pool, @variant_ids, theirs) unless theirs == ours
      end
    end

    def lines_of(camera, block)
      view.public_send("#{block}_lines", camera).map(&:to_s).reject { |line| line.start_with?('<') }
    end

    def camera_for(soc, flash_type:, layout:, edition:, interface:, sd:, mac: ETHADDR)
      camera = Camera.new(
        camera_ip_address: IPADDR, server_ip_address: SERVERIP,
        camera_mac_address: mac, flash_type: flash_type,
        firmware_version: edition, network_interface: interface, sd_card_slot: sd
      )
      camera.partition_layout = layout if layout
      camera.soc = soc
      camera
    end

    # A block is its command lines, the notes that hang under it, and whether
    # it opens with the do-not-paste warning.
    #
    # The lines are plain text: `do_not_copy_paste` returns a <span> and markup
    # in data is markup two renderers then have to agree about. It is a flag
    # here, and `firmware.installation.*` keys carry the notes -- so the static
    # wizard renders the same warning in the visitor's language rather than
    # inheriting this one's HTML.
    def blocks_for(camera)
      BLOCKS.to_h do |block|
        lines = view.public_send("#{block}_lines", camera).map(&:to_s)
        notes = view.caveats_for(lines)
        plain = lines.reject { |line| line.start_with?('<') }

        [block, pool(@pool, @pool_ids, {
          'lines' => plain,
          'notes' => notes,
          'no_paste' => lines.size != plain.size,
        })]
      end
    end

    # The warning keys that apply, in the order the controller would raise
    # them. Sentences are the renderer's job; see cameras.socs.warnings.*.
    def warnings_for(soc, flash_type:, layout:, edition:)
      keys = []
      keys << 'nothing_published' if soc.available_releases(flash_type.start_with?('nor') ? 'nor' : 'nand').empty?

      if layout == 'nor8m' && edition == 'ultimate'
        published = soc.available_releases('nor')
        unless published.empty?
          keys << if published.include?('lite')
                    flash_type == 'nor8m' ? 'eight_meg_chip' : 'eight_meg_layout'
                  else
                    flash_type == 'nor8m' ? 'no_lite_chip' : 'no_lite_layout'
                  end
        end
      end

      keys
    end

    # A view context, because the command lines live in a helper and a helper
    # needs one. Built once: instantiating it per combination turned a 30-second
    # export into a five-minute one.
    def view
      @view ||= ApplicationController.new.tap { |c| c.request = ActionDispatch::TestRequest.create }
                                     .view_context
    end
  end
end
