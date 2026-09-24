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
  # The MAC is normalised to lowercase without separators inside a filename and
  # printed with colons inside `setenv ethaddr`, so it gets two holes rather
  # than one.
  ETHADDR = '{{ethaddr}}'

  # The blocks that carry commands. Each is a helper that returns lines; the
  # page wraps them in terminal chrome and the static wizard will do the same.
  BLOCKS = %w[
    firmware_backup flashing_everything flashing_uboot flashing_linux
    preparing_environment post_flash_environment restore_from_backup
  ].freeze

  OUT_DIR = 'frontend/apps/site/src/data/wizard'

  class << self
    # One SoC, every combination its menu can offer.
    def document(soc)
      {
        'soc' => soc.urlname,
        'model' => soc.model,
        'vendor' => soc.vendor.urlname,
        'load_address' => soc.load_address,
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
        'combinations' => combinations(soc),
      }
    end

    def json(soc)
      "#{JSON.pretty_generate(document(soc))}\n"
    end

    def path(soc, root: Rails.root)
      File.join(root, OUT_DIR, "#{soc.urlname}.json")
    end

    def write_all(root: Rails.root)
      FileUtils.mkdir_p(File.join(root, OUT_DIR))
      Soc.includes(:vendor).find_each.map do |soc|
        File.write(path(soc, root: root), json(soc))
        [soc.urlname, document(soc)['combinations'].size]
      end
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

    def editions_for(soc, flash_type, layout)
      published = soc.available_releases(flash_type.start_with?('nor') ? 'nor' : 'nand')
      return published if published.empty?

      # The Ultimate-on-8MB rule, from narrow_to_what_the_menu_offers: dropped
      # only when there is a Lite to fall back to, because naming a tarball
      # upstream never built is worse than the size warning.
      if layout == 'nor8m' && published.include?('lite')
        published - ['ultimate']
      else
        published
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
        'blocks' => blocks_for(camera),
        'warnings' => warnings_for(soc, flash_type: flash_type, layout: layout, edition: edition),
      }
    end

    def camera_for(soc, flash_type:, layout:, edition:, interface:, sd:)
      camera = Camera.new(
        camera_ip_address: IPADDR, server_ip_address: SERVERIP,
        camera_mac_address: ETHADDR, flash_type: flash_type,
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

        [block, {
          'lines' => plain,
          'notes' => notes,
          'no_paste' => lines.size != plain.size,
        }]
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
