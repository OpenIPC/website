# frozen_string_literal: true

require 'rubygems/package'

class Soc < ApplicationRecord
  belongs_to :vendor

  before_validation :generate_urlname
  validates :model, presence: true, uniqueness: { scope: :vendor_id }
  validates :urlname, presence: true, uniqueness: true

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

  # Rails hands `find` whatever came out of the URL, and `to_param` returns the
  # slug, so a slug has to resolve first; ids still work, for old links and for
  # the admin forms that pass one.
  #
  # This raises rather than returning nil, because that is what every caller
  # already assumes: `#{model}.find(params[:id])` followed by a method call on
  # the result. Returning nil turned an unknown slug into a NoMethodError on
  # nil deep inside the request -- /cameras/vendors/ingenic/socs/t31 answered
  # 500 where t31x answered with firmware, because there is no SoC called
  # plain "t31". RescueHandler already turns RecordNotFound into a 404 page.
  def self.find(id)
    find_by_param(id) ||
      raise(ActiveRecord::RecordNotFound,
            "Couldn't find #{name} with urlname or id #{id.inspect}")
  end

  # The nil-returning half, for callers where the identifier is an optional
  # filter rather than the thing being addressed.
  def self.find_by_param(id)
    return nil if id.blank?

    find_by(urlname: id) || find_by(id: id)
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

  def fw_url(version)
    filename = linux_filename.dup
    filename.gsub('-br.tgz', "-#{version}-br.tgz") unless version.eql?('lite')
    format GH_DL_ROOT, filename
  end

  def full_name
    [vendor.name, model].join(' ')
  end

  def instructable?
    !uboot_filename.empty? && !linux_filename.empty?
  end

  def kernel_file
    @kernel_file ||= "uImage.#{board}"
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
    @board ||= canonical_board(linux_filename.to_s[BOARD_FROM_FILENAME, 1] || family_board)
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
  # openipc.<soc>-br.tgz scheme, which is what db/seeds.rb carries for all 48
  # of its entries. Production rows are all on the current scheme, but a fresh
  # install has no modern name to read, so dropping this rule would have T31X
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
    @rootfs_file ||= "rootfs.squashfs.#{board}"
  end

  def uboot_file
    @uboot_file ||= release_asset(uboot_filename)
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

  # uboot_filename and linux_filename are columns an admin edits, and both end
  # up here. The cache branch refuses a name that is not in the release index,
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

  private

  def generate_urlname
    self.urlname = model.downcase.gsub(' ', '-')
  end
end
