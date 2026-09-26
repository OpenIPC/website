# frozen_string_literal: true

require 'rubygems/package'

# A system-on-chip, as its vendor's file in data/catalogue describes it (#289).
# Not a table any more: see Catalogue.
class Soc
  include ActiveModel::Model
  include ActiveModel::Attributes

  ATTRIBUTES = %w[family model version status load_address sdk kernel uboot_filename
                  linux_filename notes urlname build_status_url featured segment].freeze

  attribute :family, :string
  attribute :model, :string
  attribute :version, :string
  attribute :status, :string
  attribute :load_address, :string
  attribute :sdk, :string
  attribute :kernel, :string
  attribute :uboot_filename, :string, default: ''
  attribute :linux_filename, :string, default: ''
  attribute :notes, :string
  attribute :urlname, :string
  attribute :build_status_url, :string
  attribute :featured, :boolean, default: false
  attribute :segment, :string

  attr_accessor :vendor

  # A urlname is an address and a filename (#162, #161): it is the path segment
  # under /cameras/vendors/, the name of a prerendered directory, and the name
  # of a file the wizard export writes. Derived from the model when a file does
  # not give one, which only downcases and replaces spaces -- so a model with a
  # slash or a dot in it produced a slug that walks out of every one of those.
  # Refused here, where all three read it, rather than sanitised at each.
  URLNAME_FORMAT = /\A[a-z0-9][a-z0-9._-]*\z/

  validates :vendor, presence: true
  validates :model, presence: true
  validates :urlname, presence: true, format: { with: URLNAME_FORMAT, message: 'is not a safe slug' }

  def initialize(attributes = {})
    super
    self.urlname = model.to_s.downcase.gsub(' ', '-') if urlname.blank?
    # A file that says `uboot_filename: null` means the same as leaving it out.
    self.uboot_filename ||= ''
    self.linux_filename ||= ''
  end

  RELEASES_ROOT = '/srv/github-releases'
  GH_DL_ROOT = 'https://github.com/OpenIPC/firmware/releases/download/latest/%s'

  STATUS = {
    "neq": 'No equipment on hands',
    "rnd": 'Research and development',
    "hlp": 'Looking for help',
    "wip": 'Work in progress',
    "mvp": 'Minimum viable product',
    "done": 'Done and done!'
  }.freeze

  # What kind of product this chip ends up in, which decides what the download
  # step offers besides the file (#190). `unknown` is the generic case and the
  # default for a null column, so a chip nobody has classified still gets the
  # neutral line rather than nothing.
  SEGMENTS = %w[fpv cctv consumer unknown].freeze

  # Reading, rather than the attribute, because null means "nobody has said"
  # and every caller wants the generic copy in that case. The validation below
  # keeps junk out of the catalogue; this keeps anything that reaches the model
  # some other way from reaching a translation key and rendering "translation
  # missing" into the page.
  def segment_name
    value = segment.to_s
    SEGMENTS.include?(value) ? value : 'unknown'
  end

  # Blank is the honest unclassified state and stays allowed. Anything else has
  # to be a segment: without this a typo in a catalogue file would load, and
  # `unknown` would then mean both "nobody has looked at this chip" and
  # "someone typed cctvv", which are not the same thing and want different
  # follow-up.
  validates :segment, inclusion: { in: SEGMENTS }, allow_blank: true

  def self.all
    Catalogue.current.socs
  end

  def self.count
    Catalogue.current.vendors.sum { |vendor| vendor.socs.size }
  end

  def self.featured
    all.select(&:featured)
  end

  # Rails hands `find` whatever came out of the URL, and `to_param` returns the
  # slug.
  #
  # This raises rather than returning nil, because that is what every caller
  # already assumes: `#{model}.find(params[:id])` followed by a method call on
  # the result. Returning nil turned an unknown slug into a NoMethodError on
  # nil deep inside the request -- /cameras/vendors/ingenic/socs/t31 answered
  # 500 where t31x answered with firmware, because there is no SoC called
  # plain "t31". RescueHandler already turns RecordNotFound into a 404 page.
  #
  # Numeric ids no longer resolve: they were a database's, and the catalogue
  # has none.
  def self.find(id)
    find_by_param(id) ||
      raise(ActiveRecord::RecordNotFound, "Couldn't find #{name} with urlname #{id.inspect}")
  end

  # The nil-returning half, for callers where the identifier is an optional
  # filter rather than the thing being addressed.
  def self.find_by_param(id)
    return nil if id.blank?

    Catalogue.current.soc(id)
  end

  def model_downcase
    @model_downcase ||= model.downcase
  end

  def to_param
    urlname
  end

  def bl_url
    format GH_DL_ROOT, uboot_filename
  end

  # Not used by any view today. It returned the lite filename for every version
  # asked for -- `gsub` without `!` and without assigning the result -- and it
  # still spoke the pre-2023 `-br.tgz` naming, so it would have answered a dead
  # URL for whoever reached for it next. Built from the same rule as everything
  # else now.
  def fw_url(release, flash_type = 'nor')
    format GH_DL_ROOT, linux_filename_for(release, flash_type)
  end

  def full_name
    [vendor.name, model].join(' ')
  end

  def instructable?
    !uboot_filename.empty? && !linux_filename.empty?
  end

  FLASH_TYPES = %w[nor nand].freeze

  # Display order for the editions we know about. Not a filter: anything else
  # upstream publishes is offered too, after these, so a new variant does not
  # wait on a deploy here. `fabricator` is deliberately absent -- it was offered
  # for every SoC for years and upstream has never published one.
  RELEASE_ORDER = %w[lite ultimate neo].freeze

  # The editions upstream actually publishes for this board and flash type.
  #
  # The site used to offer lite, ultimate and fabricator for all 126 SoCs
  # regardless. Ultimate does not exist for 42 of them, fabricator exists for
  # none, and neo -- which does exist, for seven boards -- could not be reached
  # at all. Every one of those was a dead download the visitor only discovered
  # after choosing, submitting and clicking.
  #
  # Falling back to the known list keeps the old behaviour when the index
  # cannot be read, rather than emptying the menu.
  def available_releases(flash_type)
    published_releases(flash_type)
  rescue ReleaseIndex::Missing
    # Offer the usual three rather than an empty menu. Some of them may not
    # exist for this board -- that is the whole problem being fixed -- but a
    # menu with nothing in it makes every SoC unusable for as long as the index
    # is unreadable, and the download path answers "could not be fetched right
    # now" rather than pretending. Anything that would put a bare GitHub URL in
    # front of a visitor uses published_releases instead, which stays silent.
    RELEASE_ORDER
  end

  # Strictly what the index says, and nothing when it cannot be read.
  def published_releases(flash_type)
    ReleaseIndex.current.releases_for(board, flash_type)
                .sort_by { |release| [RELEASE_ORDER.index(release) || RELEASE_ORDER.size, release] }
  end

  # Every edition offerable for this SoC, across flash types. The menu carries
  # the union because the flash type is chosen in the same form, without a
  # round trip, and the script narrows it from there.
  def offerable_releases
    FLASH_TYPES.flat_map { |flash_type| available_releases(flash_type) }
               .uniq
               .sort_by { |release| [RELEASE_ORDER.index(release) || RELEASE_ORDER.size, release] }
  end

  # What the flash-type menu and the script need: which editions go with which
  # flash type, for this SoC.
  def release_availability
    FLASH_TYPES.to_h { |flash_type| [flash_type, available_releases(flash_type)] }
  end

  # nor8m suits almost every SoC and is wrong for the ones upstream builds only
  # a NAND image for -- rv1109 and rv1126 here today. Their NOR sizes are
  # disabled in the menu, so opening the form on one left it with a flash type
  # that cannot be chosen and no edition to go with it.
  def default_flash_chip
    available_releases('nor').any? ? 'nor8m' : 'nand'
  end

  # For the links out to GitHub, which have to name a file that is there. An
  # unreadable index means no links rather than links built on a guess: a 404 on
  # github.com is off-site, with nothing from this site to explain it.
  def published_availability
    FLASH_TYPES.to_h { |flash_type| [flash_type, published_releases(flash_type)] }
  rescue ReleaseIndex::Missing
    FLASH_TYPES.to_h { |flash_type| [flash_type, []] }
  end

  # A flash image starts with a bootloader, and for twenty-one of the SoCs on
  # this site there is not one. Every Xiongmai part is like this, and the whole
  # GK7102 family: OpenIPC builds and publishes firmware for them, but no
  # u-boot, so the installation instructions and the assembled image cannot be
  # offered however much of the rest exists.
  #
  # Worth distinguishing from "not supported yet", which is what the SoC page
  # used to say for all of them. It is wrong twice over -- it claims firmware is
  # coming when it is already published and linked on that same page, and it
  # hides the one thing a visitor could act on, which is that they will need
  # their camera's own bootloader.
  # What a visitor can actually do with this chip (#189).
  #
  # The catalogue lists 126 SoCs and the lists said one of two things: "Generate
  # guide", or "No solution yet" for everything else. That second string covered
  # a chip with published firmware and no bootloader -- which is installable,
  # through the stock bootloader -- and a chip nobody has hardware for, and read
  # the same either way. Ten of the fourteen vendor tabs contain no installable
  # chip at all and took 55% of vendor-page views in the 19-20 September sample.
  # People open these pages and leave with nothing.
  #
  #   :wizard         bootloader and firmware published -- the wizard builds an
  #                   image. 55 chips.
  #   :firmware_only  firmware but no bootloader -- flash the bundle through the
  #                   camera's own bootloader; the wizard can never help. 22.
  #   :none           nothing published. 49, and `status` says why: 25 neq (no
  #                   equipment), 16 rnd, 6 wip, 2 hlp (looking for help).
  #
  # Memoised because the lists ask for every row, and both predicates read
  # ReleaseIndex -- which is already in memory, but the fetch and the
  # releases_for scan are not free across 126 rows. Against the index, not the
  # instance: a catalogue record lives as long as the process, and the index is
  # refreshed hourly.
  #
  # #161 carries these states into the YAML catalogue and #162 into the
  # prerendered pages, so this is the one definition all three read.
  def availability
    from_index(:availability) do
      if firmware_published?
        bootloader_published? ? :wizard : :firmware_only
      else
        :none
      end
    end
  end

  def installable?
    availability != :none
  end

  def bootloader_published?
    return false if uboot_filename.blank?

    !ReleaseIndex.current.fetch(uboot_filename).nil?
  rescue ReleaseIndex::Missing
    # No index is not evidence of absence, and claiming a bootloader is missing
    # when we simply cannot see it would be worse than saying nothing.
    true
  end

  # Asked against the index directly rather than through available_releases,
  # which answers optimistically when there is no index. Optimism is right for
  # a menu -- offer the usual editions rather than an empty one -- and wrong
  # here, where the answer decides whether to tell somebody firmware exists.
  def firmware_published?
    index = ReleaseIndex.current
    FLASH_TYPES.any? { |flash_type| index.releases_for(board, flash_type).any? }
  rescue ReleaseIndex::Missing
    linux_filename.present?
  end

  def kernel_file
    "uImage.#{board}"
  end

  # The board a firmware is built for, which is not always the SoC model.
  # Ingenic ships one build per family (T31X, T31N and the rest all flash
  # openipc.t31-*), Goke does the same across the GK7102 variants, and
  # AK3916EV301 runs the AK3918EV200 build outright. None of that is derivable
  # from the model string, so it is read out of linux_filename, which carries
  # the name upstream actually publishes.
  #
  # This used to be guessed as model.downcase with hardcoded exceptions for
  # t31 and t40 only. T23N therefore asked for openipc.t23n-nor-lite.tgz, a
  # file that has never existed -- 31 of the 99 failed firmware downloads in a
  # fortnight, the largest single cause. T30L was the same bug.
  BOARD_FROM_FILENAME = /\Aopenipc\.(.+)-(?:nor|nand)-[a-z0-9]+\.tgz\z/

  # Families that ship one build for every model in them. Only consulted when
  # linux_filename cannot answer; it covers more cases than this list can.
  FAMILY_BUILDS = %w[t31 t40 t30 t23].freeze

  def board
    from_index(:board) { canonical_board(linux_filename.to_s[BOARD_FROM_FILENAME, 1] || family_board) }
  end

  # What linux_filename names is the chip this SoC is; what upstream builds may
  # be another board entirely. GK7205V210 has not been built since 2026-06-07 --
  # it is firmware-identical to GK7205V200 and served from it -- so a row saying
  # openipc.gk7205v210-nor-lite.tgz names a tarball that no longer exists, and
  # asking for it is a dead download rather than a stale one.
  #
  # Resolving here rather than at the point of download is deliberate: the board
  # also names the members inside the tarball (uImage.<board>,
  # rootfs.squashfs.<board>) and the bundle link on the SoC page. Substituting
  # only the filename would fetch the right tarball and then fail to find
  # anything in it.
  #
  # Unresolvable is not an error. Without an index this answers what the column
  # says, which is what it did before the map existed and is right for every SoC
  # that has no alias.
  def canonical_board(board)
    ReleaseIndex.current.canonical_board(board)
  rescue ReleaseIndex::Missing
    board
  end

  # Reached when linux_filename is blank or still in the pre-2023
  # openipc.<soc>-br.tgz scheme. The catalogue is on the current scheme
  # throughout, but dropping this rule would have T31X with an old-style name
  # ask for a t31x build that upstream has never published.
  def family_board
    FAMILY_BUILDS.find { |family| model_downcase.start_with?(family) } || model_downcase
  end

  # Not memoised: it takes arguments, and `@linux_file ||=` returned the first
  # call's path for every later one regardless of what was asked for.
  # The name upstream publishes for this board, edition and flash type. Split
  # out from linux_file because composing a name and finding the file are two
  # different questions: the first is pure and worth testing on its own, the
  # second reaches a disk or the network.
  def linux_filename_for(release, flash_type)
    "openipc.#{board}-#{flash_type}-#{release}.tgz"
  end

  def linux_file(release, flash_type)
    release_asset linux_filename_for(release, flash_type)
  end

  def rootfs_file
    "rootfs.squashfs.#{board}"
  end

  def uboot_file
    release_asset(uboot_filename)
  end

  # Where an upstream asset can be read from.
  #
  # RELEASE_MIRROR_ROOT names the directory the hourly cron fills. While it is
  # set and holds the file, it wins: that is how this was cut over, and how it
  # is cut back if the cache turns out to be a mistake -- one line of
  # environment and a restart, no deploy.
  #
  # Without it, or for a file the mirror does not have, ReleaseCache fetches it
  # on demand. That raises rather than returning a path to nothing:
  # UnknownAsset for a name upstream is not publishing, Unavailable when it
  # cannot be had right now. Cameras::SocsController answers for both.
  def release_asset(name)
    plain_asset_name!(name)

    root = ENV['RELEASE_MIRROR_ROOT'].presence
    if root
      mirrored = File.join(root, name)
      return mirrored if File.exist?(mirrored) && within?(root, mirrored)
    end

    ReleaseCache.path(name)
  end

  # uboot_filename and linux_filename come from a catalogue file anyone can
  # propose a change to, and both end up here. The cache branch refuses a name that is not in the release index,
  # but the mirror branch has no index to consult, so it needs its own answer:
  # without one, uboot_filename of "../../../etc/passwd" resolves outside the
  # mirror root, Firmware#assemble reads it as the bootloader, and
  # download_full_image sends the result.
  #
  # The rule is the one deploy/mirror-releases.rb applies at the other end --
  # unchanged by basename, so no separators and no ".." or "."; no leading dot,
  # so nothing collides with the index or the state file; no control
  # characters. Structural rather than a list of permitted characters, for the
  # reason recorded there: guessing at the character set upstream is allowed to
  # use is how you refuse files you meant to keep.
  def plain_asset_name!(name)
    value = name.to_s
    # Control characters first, and not merely for tidiness: a NUL is one, and
    # File.basename raises ArgumentError on a string containing one rather than
    # returning something to compare. Checking here means the cheap test
    # rejects it before any path arithmetic sees it.
    ok = !value.empty? &&
         !value.match?(/[[:cntrl:]]/) &&
         value == File.basename(value) &&
         !value.start_with?('.')
    return if ok

    raise ReleaseCache::UnknownAsset, "#{value.inspect} is not a plain asset name"
  rescue ArgumentError
    # Whatever else File.basename dislikes about it, the answer is the same.
    raise ReleaseCache::UnknownAsset, "#{value.inspect} is not a plain asset name"
  end

  # Belt and braces behind the check above: whatever File.join produced has to
  # sit under the root it was joined to.
  def within?(root, path)
    base = File.expand_path(root)
    File.expand_path(path).start_with?("#{base}/")
  end

  def full_firmware_path
    @full_firmware_path ||= "/tmp/openipc.#{model_downcase}.8mb.bin"
  end

  # A value read off the release index, remembered for as long as that index
  # is the current one. The catalogue's records outlive any one request -- they
  # are loaded once per process -- so memoising on the instance, as these did
  # when every request loaded its own rows, would answer from the index that
  # was current at boot. ReleaseIndex.current hands back the same object until
  # the file changes, so identity is the test. No index is a state too: nil.
  def from_index(key)
    index = begin
      ReleaseIndex.current
    rescue ReleaseIndex::Missing
      nil
    end
    @from_index = [index, {}] unless @from_index && @from_index.first.equal?(index)
    memo = @from_index.last
    memo.key?(key) ? memo[key] : (memo[key] = yield)
  end
  private :from_index

  def ==(other)
    other.is_a?(Soc) && other.urlname == urlname
  end
  alias eql? ==

  def hash
    urlname.hash
  end
end
