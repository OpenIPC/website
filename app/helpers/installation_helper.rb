# frozen_string_literal: true

module InstallationHelper
  # The block, plus a way out for the readers whose bootloader cannot run it.
  #
  # guarded_flash joins the transfer to the erase and the write with `&&` so a
  # failed transfer cannot reach the erase -- see the comment on that method for
  # the camera that cost. But `&&` is a hush feature, and stock vendor
  # bootloaders are what people are running when they follow this page for the
  # first time. At least one does not have it: the reporter in
  # OpenIPC/firmware#2299 got "the help entry for the first command" back and
  # had to enter each part by hand.
  #
  # That failure is safe -- nothing is transferred, nothing is erased -- but it
  # is silent about why, and the line is not one a reader can take apart
  # unaided. So say it, next to the block it applies to, and only there.
  #
  # Saying it was not enough on its own. The note used to leave the reader to
  # work out how many parts the line has, and the reporter in
  # OpenIPC/firmware#2381 counted two: they re-entered the transfer and the
  # write and dropped the `sf erase` between them. NOR programming only clears
  # bits, so the write stored `old AND new` over the whole chip, said
  # "100% complete", and left the bootloader unable to run -- no serial output,
  # no link, and nothing anywhere that named the missing step. Hence the note
  # counts the parts out and says the erase cannot be the one left out.
  #
  # `sf lock 0` raises the same question one line earlier, and answering it
  # matters for the same reason: the reporter in OpenIPC/firmware#2405 got the
  # `sf` usage help back from a stock hi3516ev200 bootloader and stopped there.
  # That subcommand is OpenIPC U-Boot's -- a stock one built from mainline has
  # `sf protect` and no `lock` -- so the failure is expected and harmless, but
  # nothing on the page said so. What it loses is the check: flash that is
  # still protected discards an erase and a write while reporting success. So
  # the note has to name the way out as well -- that bootloader cannot clear
  # the protection, and a reader who only gets the diagnosis is left with a
  # camera they still cannot flash.
  #
  def list_of_commands(text)
    notes = caveats_for(text).map do |key|
      content_tag('p', t("firmware.installation.#{key}"), class: 'small text-body-secondary')
    end
    notes.empty? ? terminal_block(text) : safe_join([terminal_block(text), *notes])
  end

  # The site's terminal chrome, deliberately without its copy button.
  #
  # shared/_terminal is the component every other page uses for a command, and
  # these blocks were a bare `pre.bg-light` from before it existed -- light
  # grey, no frame, indistinguishable from a quotation on the one page where
  # the difference between prose and a command matters most.
  #
  # But the partial ships a copy-to-clipboard button, and here that would be a
  # trap. Every one of these blocks opens with "Enter commands line by line! Do
  # not copy and paste multiple lines at once!", and the reason is in
  # guarded_flash above: a bootloader that does not understand `&&` runs the
  # whole pasted line as one command, or runs the write without the erase.
  # Offering one click that puts all of it on the clipboard, on a page where
  # that bricks a camera, is not a convenience.
  #
  # The wrapping is inherited and wanted: .terminal wraps rather than scrolls,
  # so a long tftpboot line stays readable to its end instead of disappearing
  # behind a scrollbar most systems do not draw.
  def terminal_block(text)
    header = content_tag('div', class: 'terminal-header') do
      safe_join([
        content_tag('span', safe_join([tag.span, tag.span, tag.span]), class: 'dots'),
        content_tag('span', t('firmware.installation.shell_label'), class: 'text-data')
      ])
    end
    body = content_tag('pre', content_tag('code', text.join('<br>').html_safe))
    content_tag('div', safe_join([header, body]), class: 'terminal my-3')
  end

  # The line that raises a question, and the note that answers it. One table
  # rather than a growing ladder of ifs, because what has to stay true of this
  # is a property of the whole set: insertion order is the order the notes come
  # out in, and it matches the order the lines appear in a block, so a reader
  # working down one meets each note where it goes wrong.
  CAVEATS = { 'printenv ethaddr' => 'mac_record_caveat_html',
              'sf lock' => 'lock_caveat_html',
              '&&' => 'compound_caveat_html' }.freeze

  # Which notes a block has earned.
  def caveats_for(text)
    lines = text.map(&:to_s)
    CAVEATS.select { |trigger, _| lines.any? { |line| line.include?(trigger) } }.values
  end

  # The one line of every command block that is not a command, and the one a
  # reader most needs to understand -- guarded_flash's whole reason. It was
  # English for everyone until #164 needed it on the static wizard too, which
  # is where the key came from.
  def do_not_copy_paste
    content_tag 'span', t('firmware.installation.do_not_paste'), class: 'text-danger'
  end

  # `sf probe` has to run before any erase, and `sf lock 0` clears the status
  # register block protection some vendors arm. Kept as its own line rather
  # than chained into the flashing command: plenty of vendor U-Boots have no
  # `sf lock` subcommand at all, and a chain would abort on their error
  # instead of going on to flash. Those are the bootloaders the note in
  # list_of_commands is for -- see OpenIPC/firmware#2405.
  def unlock_flash(text, c)
    text << 'sf probe 0; sf lock 0;' unless c.flash_type.eql?('nand')
  end

  # Build one line that only erases if the transfer before it succeeded.
  #
  # Both halves matter. U-Boot's `&&` stops a failed `tftpboot`/`tftp`/
  # `fatload` from reaching the erase, and `${filesize}` bounds the write to
  # what actually arrived. Without them a transfer that fails, or one whose
  # load address was mistyped -- U-Boot's hex parser stops at the first
  # non-hex character, so `0mx82000000` silently becomes `0` -- still erased
  # the chip and wrote back the `mw.b` fill, putting 0xff over the
  # bootloader while reporting success. See OpenIPC/website#55 and the
  # camera it destroyed in OpenIPC/firmware#2299.
  #
  # It must stay a single line, because the block above it tells the user to
  # enter commands one at a time.
  def guarded_flash(c, transfer, offset, erase_size, write_size)
    cmd = c.flash_type.eql?('nand') ? 'nand' : 'sf'
    "#{transfer} && #{cmd} erase #{offset} #{erase_size} " \
      "&& #{cmd} write #{c.soc.load_address} #{offset} #{write_size}"
  end

  # NOR writes only what arrived. NAND keeps the fixed partition size it has
  # always used, since `${filesize}` is not guaranteed to be page-aligned.
  def write_size_for(c, fixed)
    c.flash_type.eql?('nand') ? fixed : '${filesize}'
  end

  # The dump, and the one thing the dump does not carry.
  #
  # A full-image install erases the whole chip, so whatever the stock firmware
  # kept its MAC in goes with it. On first boot the camera looks for an address
  # of its own, then for the factory one still in flash (`ipcinfo --xm-mac`,
  # NOR and the Xiongmai layout only), and only then mints itself a locally
  # administered `02:` one -- so after a whole-chip erase there is nothing left
  # for step two to find, and the factory address is gone for good unless
  # somebody wrote it down. It costs one command to write it down here, beside
  # the backup it belongs with. OpenIPC/firmware#2405.
  def firmware_backup(c)
    list_of_commands(firmware_backup_lines(c))
  end

  # The lines themselves, so #163's export can carry them: one producer
  # for the page and for the data the static wizard will render from.
  def firmware_backup_lines(c)
    text = []
    text << do_not_copy_paste
    text << 'printenv ethaddr'
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff #{c.flash_size_hex}"
    if c.flash_type.eql?('nand')
      text << "nand read #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    else
      text << "sf probe 0; sf read #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    end

    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << "mmc dev 0; mmc erase 0x10 #{c.flash_size_blocks}; mmc write #{c.soc.load_address} 0x10 #{c.flash_size_blocks}"
      text << ""
      text << "# Use the following command to restore the backup to a file on a PC"
      text << "# (replace /dev/sdc with your SD card device):"
      # backup_filename, not a fixed `fulldump.bin`. Two reasons, and the
      # second one predates the first: this path collides across cameras
      # exactly like the tftpput path did -- see backup_filename and
      # OpenIPC/firmware#2405 -- and the restore block below has always loaded
      # `backup_filename` off the card, so a reader who followed both wrote one
      # name and was then sent after another. Review on #140.
      text << "# sudo dd bs=512 skip=16 count=#{c.flash_size_sectors} if=/dev/sdc of=./#{c.backup_filename}"
    else
      text << "tftpput #{c.soc.load_address} #{c.flash_size_hex} #{c.backup_filename}"
      text << '# if there is no tftpput but tftp then run this instead'
      text << '# (the third argument is what makes tftp upload rather than download)'
      text << "tftp #{c.soc.load_address} #{c.backup_filename} #{c.flash_size_hex}"
    end
    text
  end

  def flashing_everything(c)
    list_of_commands(flashing_everything_lines(c))
  end

  # The lines themselves, so #163's export can carry them: one producer
  # for the page and for the data the static wizard will render from.
  def flashing_everything_lines(c)
    fw_filename = Firmware.filename_for(soc_model: c.soc.model_downcase, flash_type: c.flash_type_type,
                                        release: c.firmware_version, size: c.flash_size,
                                        layout: c.layout_size)
    # The full image is exactly the size it claims on NOR and page-aligned by
    # construction on NAND, so ${filesize} is always a safe write length here --
    # unlike the u-boot-only block below, where the binary is neither.
    #
    # And on NOR that size is the chip's, whatever partition layout is going
    # inside it, so the erase below spans the whole part. It used to span the
    # size of the layout instead, which is the same number for every
    # combination the menu could produce until it grew a second field -- and
    # then, for an 8MB layout on a 16MB chip, left the top half of the flash
    # untouched. The overlay ends `-(rootfs_data)` and so runs to the end of
    # the device: a jffs2 that survives up there is mounted on the next boot
    # and the camera comes back exactly as broken as it went in.
    write_size = '${filesize}'
    text = []
    text << do_not_copy_paste
    text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    text << "mw.b #{c.soc.load_address} 0xff #{c.staging_size_hex}"
    unlock_flash text, c
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{fw_filename}",
                            '0x0', c.flash_size_hex, write_size)
    else
      text << guarded_flash(c, "tftpboot #{c.soc.load_address} #{fw_filename}",
                            '0x0', c.flash_size_hex, write_size)
      text << '# if there is no tftpboot but tftp then run this instead'
      text << guarded_flash(c, "tftp #{c.soc.load_address} #{fw_filename}",
                            '0x0', c.flash_size_hex, write_size)
    end
    text << 'reset'
    text
  end

  def flashing_uboot(c)
    list_of_commands(flashing_uboot_lines(c))
  end

  # The lines themselves, so #163's export can carry them: one producer
  # for the page and for the data the static wizard will render from.
  def flashing_uboot_lines(c)
    write_size = write_size_for(c, '0x50000')
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff 0x50000"
    unlock_flash text, c
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.uboot_filename}",
                            '0x0', '0x50000', write_size)
    else
      text << guarded_flash(c, "tftpboot #{c.soc.load_address} #{c.soc.uboot_filename}",
                            '0x0', '0x50000', write_size)
      text << '# if there is no tftpboot but tftp then run this instead'
      text << guarded_flash(c, "tftp #{c.soc.load_address} #{c.soc.uboot_filename}",
                            '0x0', '0x50000', write_size)
    end
    text << 'reset'
    text
  end

  # The suffix comes off the camera rather than being passed in beside it: the
  # macros are named for the bootloader's own environment, and on SigmaStar and
  # Ingenic that is `uknor`/`urnor` with nothing after it whatever layout is
  # being installed.
  def flashing_linux(c)
    list_of_commands(flashing_linux_lines(c))
  end

  # The lines themselves, so #163's export can carry them: one producer
  # for the page and for the data the static wizard will render from.
  def flashing_linux_lines(c)
    c2 = c.bootloader_macro_suffix
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
      text << "setenv ethaddr #{c.camera_mac_address}" if c.mac_address_command?
      text << 'saveenv'
    end
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << "mw.b #{c.soc.load_address} 0xff 0x200000"
      unlock_flash text, c
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.kernel_file}",
                            c.kernel_offset, c.kernel_max_size, '${filesize}')
      text << ''
      text << "mw.b #{c.soc.load_address} 0xff 0x500000"
      unlock_flash text, c
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.rootfs_file}",
                            c.rootfs_offset, c.rootfs_max_size, '${filesize}')
      text << ''
    else
      text << "run uk#{c2}; run ur#{c2}"
    end
    # No overlay erase on NAND: with mtdpartsubi everything past the kernel is
    # one `ubi` partition and rootfs_data is a volume inside it, so a raw erase
    # at an offset would cut into UBI. `urnand` already erases that whole
    # partition before writing. This is also what used to render the malformed
    # `nand erase 0xD50000 0x-550000`.
    text << "sf erase #{c.overlay_offset} #{c.overlay_max_size}" unless c.flash_type.eql?('nand')
    text << 'reset'
    text
  end

  # The bootloader variables the instructions above actually named, for the hint
  # that tells the reader to go and look them up. Camera builds the list from
  # the same suffix the commands are built from, so the two stay in step --
  # including the nor32m -> nor16m rewrite and the vendors whose macros carry no
  # suffix and have no `set…` to name.
  #
  # It used to be a fixed `uknor*, urnor*, setnor*`, which named nothing a NAND
  # reader had been given and nothing they could find in their own printenv.
  def bootloader_variables_html(camera)
    safe_join(camera.bootloader_variables.map { |name| tag.code(name) }, ', ')
  end

  # Put the bootloader on the layout that was just flashed. One macro where
  # there is one, and the `setenv` that macro would have done where there is
  # not -- see Camera#layout_commands. Nothing at all when the layout is already
  # the bootloader's default, which is why every caller checks first.
  def preparing_environment(camera)
    list_of_commands(preparing_environment_lines(camera))
  end

  # The lines themselves, so #163's export can carry them: one producer
  # for the page and for the data the static wizard will render from.
  def preparing_environment_lines(camera)
    text = []
    text << do_not_copy_paste
    text.concat(camera.layout_commands)
    text
  end

  # The full-image path's own environment step. Kept apart from
  # preparing_environment, which the by-parts path uses: there flashing_linux
  # has already run `setenv ethaddr`, and U-Boot refuses a second one on a
  # variable that is now set.
  def post_flash_environment(camera)
    list_of_commands(post_flash_environment_lines(camera))
  end

  # The lines themselves, so #163's export can carry them: one producer
  # for the page and for the data the static wizard will render from.
  def post_flash_environment_lines(camera)
    text = []
    text << do_not_copy_paste
    text.concat(camera.post_flash_commands)
    text
  end

  def restore_from_backup(c)
    list_of_commands(restore_from_backup_lines(c))
  end

  # The lines themselves, so #163's export can carry them: one producer
  # for the page and for the data the static wizard will render from.
  def restore_from_backup_lines(c)
    write_size = write_size_for(c, c.flash_size_hex)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff #{c.staging_size_hex}"
    unlock_flash text, c
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.backup_filename}",
                            '0x0', c.flash_size_hex, write_size)
    else
      text << guarded_flash(c, "tftpboot #{c.soc.load_address} #{c.backup_filename}",
                            '0x0', c.flash_size_hex, write_size)
    end
    text
  end

  # What the download step says under the file, besides the file (#190).
  #
  # Two sentences at most, muted, below the link and never between the command
  # blocks people paste into a bootloader. Nothing stands between the visitor
  # and the download: no interstitial, no gate, no timer.
  #
  # The licence sentence is a statement about the image in front of them, so it
  # is only made where it is true. Every `done` chip's image carries Majestic
  # -- checked across all 95 assemblable images that map to a defconfig -- and
  # the twelve that carry none are every one of them wip/neq/rnd/hlp/mvp, with
  # zero downloads between them in ninety days. `status` is therefore a fact
  # the site already holds that answers the question exactly, which beats a
  # second hand-maintained list of chips that would drift the first time
  # upstream changed a defconfig and nothing here would notice.
  def download_licence_notice(camera)
    return unless camera.soc.status.to_s == 'done'

    segment = camera.soc.segment_name
    lines = [t('firmware.installation.licence_html')]
    lines << business_ask(camera, segment) unless segment == 'consumer'

    tag.div(class: 'download-licence small text-body-secondary border-top pt-3 mt-2') do
      safe_join(lines.map { |line| tag.p(line, class: 'mb-0') })
    end
  end

  # What to do once the camera is up (#191).
  #
  # The success block is the closest thing this site has to a thank-you page,
  # and it ended the visit with nothing to do. It is also the only place in the
  # wizard where an ask reads as "what next" rather than as a toll: after the
  # value is delivered, never before the download, and never on a page that
  # cannot install anything.
  #
  # Three plain links, no box inside a box, no modal and no timer. The third
  # one is the segment's ask and never both -- a visitor is asked to donate or
  # asked about commercial terms, not handed a menu of ways to pay.
  #
  # `data-whatnext` marks the list rather than a class, because the browser
  # hides it after the first camera of a tab session (see wizard.js) and a
  # class that means "style me" and "find me" at once is one rename away from
  # doing neither.
  def what_next_list(camera)
    items = [what_next_chat(camera), what_next_wall, what_next_support(camera)]

    tag.ul(class: 'list-unstyled mb-0 mt-3', data: { whatnext: true }) do
      safe_join(items.map { |item| tag.li(item, class: 'mb-1') })
    end
  end

  private

  # The business line is a question, not an offer: the visitor decides whether
  # it is about them. `?ref=` carries the attribution even when the event does
  # not -- the beacon is an async request and a same-tab navigation can cancel
  # it, where the landing page reading `?ref=` cannot be raced.
  #
  # The alternative wording ships as a data attribute rather than as a second
  # rendered variant, and the browser chooses between them from sessionStorage:
  # after #155 and #156 this page is a cacheable GET, so a server-side variant
  # keyed on anything about the visitor would make it uncacheable again.
  def business_ask(camera, segment)
    query = { ref: 'download-step', soc: camera.soc.urlname,
              edition: camera.firmware_version }.to_query
    link = link_to(t('firmware.installation.licence_business_link'),
                   "#{locale_path('/business')}?#{query}",
                   data: { event: "download-step:business:#{segment}",
                           volume_text: t('firmware.installation.licence_volume_ask'),
                           volume_link: t('firmware.installation.licence_volume_link') })

    safe_join([t("firmware.installation.licence_ask.#{segment}"), ' ', link, '.'])
  end

  # Every visitor is offered every door; the locale and the chip decide the
  # order, and nothing else.
  #
  # The rule this replaces routed on the locale: `en` and `ru` got a t.me link,
  # and `zh` got /community instead, on the stated belief that the page "lists
  # the WeChat contact alongside the rest". It does not, and the project holds
  # no WeChat account and plans none, so that redirect swapped one link a
  # Chinese visitor cannot open for a page of four more. Telegram is now
  # blocked in Russia too, which put `ru` in the same position.
  #
  # Routing on the locale was the deeper mistake, and it is why nothing here
  # filters. A language setting is not a network: ordering on it is honest,
  # because it is a hint about what to print first, but removing a door on it
  # strands the Chinese-reading FPV builder whose VPN works. So the FPV chip
  # still wins the ordering, and it can no longer suppress the warning.
  #
  # Order is least-specific first, so each `move_first` below overrides the one
  # before it.
  CHAT_ROOMS = [['en', 'https://t.me/+7LL2kc32SOo5YWYy', 'OpenIPC Users (EN)'],
                ['fpv', 'https://t.me/+BMyMoolVOpkzNWUy', 'OpenIPC & FPV'],
                ['ru', 'https://t.me/+Sl2GPoR9G2iJAOCr', 'OpenIPC Users (RU)']].freeze

  def what_next_chat(camera)
    doors = tag.ul(safe_join(chat_door_items(camera)),
                   class: 'list-unstyled ms-3 mb-0 mt-1 small', data: { chat: true })
    wayout = tag.p(t('firmware.installation.chat_blocked_html'),
                   class: 'ms-3 mb-0 mt-1 small text-body-secondary', data: { chat_fallback: true })

    # Chinese has no room of its own, so the way out is the first useful thing
    # on the list rather than a footnote under three links in other languages.
    body = I18n.locale == :zh ? [wayout, doors] : [doors, wayout]

    safe_join([t('firmware.installation.chat_lede'), *body])
  end

  # One description per room, read from the community page's own copy rather
  # than duplicated here -- these are the same four sentences, already
  # translated, and two copies would drift.
  def chat_door_items(camera)
    chat_doors(camera).map do |key, url, label|
      tag.li(safe_join([link_to(label, url, data: { event: "download-step:chat:#{key}" }),
                        ' — ', t("pages.community.channel_#{key}")]))
    end
  end

  def chat_doors(camera)
    doors = move_first(CHAT_ROOMS, I18n.locale.to_s)
    doors = move_first(doors, 'fpv') if camera.soc.segment_name == 'fpv'
    doors
  end

  # A locale with no room of its own (zh) matches nothing and leaves the order
  # alone, which is the wanted behaviour rather than a special case.
  def move_first(doors, key)
    doors.partition { |door| door.first == key }.flatten(1)
  end

  # The site's own page, not the wiki. #191 said to link the wiki's Open Wall
  # section; there is no such page, and /get-started already describes the wall
  # the same way -- one setting in the web interface.
  def what_next_wall
    what_next_line('wall', locale_path('/open-wall'), 'download-step:wall')
  end

  # Donate or commercial terms, decided by the segment and never both (#190,
  # #191). A `consumer` doorbell is not a lead, and an integrator flashing a
  # hundred CCTV cameras is not someone to pass a tip jar.
  def what_next_support(camera)
    segment = camera.soc.segment_name

    if %w[fpv cctv].include?(segment)
      href = "#{locale_path('/business')}?#{{ ref: 'download-step', soc: camera.soc.urlname,
                                              edition: camera.firmware_version }.to_query}"
      what_next_line('business', href, "download-step:business:#{segment}")
    else
      what_next_line('donate', locale_path('/donate'), 'download-step:donate')
    end
  end

  def what_next_line(key, href, event)
    safe_join([link_to(t("firmware.installation.next_#{key}_link"), href, data: { event: event }),
               ' ', t("firmware.installation.next_#{key}_text")])
  end
end
