# frozen_string_literal: true

# Writes the goldens the Go firmware builder is held to, from the Ruby
# implementation it replaces (#299). Run it with the Rails app, not in it --
# nothing under app/ changes:
#
#   bin/rails runner service/testdata/gen/firmware_golden.rb
#
# Three files, all committed under service/internal/firmware/testdata/:
#
#   boards.json     every catalogue SoC: its board, its bootloader asset, and
#                   the tarball names it would ask for -- against a snapshot of
#                   the production release index (release-index.json)
#   availability.json  the availability feed's socs map, against that index
#   manifests.json  full images, assembled by Firmware#generate from synthetic
#                   release assets, for every vendor's flash-layout table on
#                   every flash type, chip size and partition layout: the image
#                   length, its SHA-256, and where each member landed
#
# The synthetic assets are deterministic (SHA-256 in counter mode over a seed),
# so the Go test rebuilds the same inputs and must produce the same bytes. The
# image depends only on the members' contents, never on how the tarball was
# packed, so the two tarballs need not be identical.

require 'digest'
require 'json'
require 'rubygems/package'
require 'tmpdir'
require 'zlib'

OUT = Rails.root.join('service/internal/firmware/testdata')
INDEX = JSON.parse(File.read(OUT.join('release-index.json')))

def synthetic(seed, size)
  out = +''.b
  counter = 0
  while out.bytesize < size
    out << Digest::SHA256.digest("#{seed}\0#{[counter].pack('Q>')}")
    counter += 1
  end
  out.byteslice(0, size)
end

def tgz(members)
  io = StringIO.new(+''.b)
  Zlib::GzipWriter.wrap(io) do |gz|
    Gem::Package::TarWriter.new(gz) do |tar|
      members.each { |name, data| tar.add_file_simple(name, 0o644, data.bytesize) { |f| f.write(data) } }
    end
  end
  io.string
end

# The sizes. The rootfs fits every NOR layout's rootfs partition (5 MB on the
# 8 MB table); TOO_LARGE does not fit the 8 MB one, which is the Ultimate-on-8MB
# failure people actually meet.
SIZES = { 'uboot' => 200_000, 'kernel' => 1_500_000, 'rootfs' => 3_000_000 }.freeze
TOO_LARGE = 5_300_000

prod_index = Dir.mktmpdir
File.write(File.join(prod_index, '.index.json'), JSON.generate(INDEX))
ENV['RELEASE_INDEX_ROOT'] = prod_index
ReleaseIndex.reset!

# /api/v1/hardware/availability.json's socs map (#298), against the same index.
availability = Soc.all.sort_by(&:urlname).to_h { |soc| [soc.urlname, soc.availability.to_s] }

boards = Soc.all.map do |soc|
  {
    urlname: soc.urlname, vendor: soc.vendor.name, model: soc.model,
    board: soc.board, uboot: soc.uboot_filename.to_s,
    nor_lite: soc.linux_filename_for('lite', 'nor'), nand_lite: soc.linux_filename_for('lite', 'nand')
  }
end

# The tarball for one board, flash type and rootfs.
def linux_tarball(board, flash, kernel, root)
  member = flash == 'nand' ? "rootfs.ubi.#{board}" : "rootfs.squashfs.#{board}"
  tgz("uImage.#{board}" => kernel, member => root, "#{member}.md5sum" => 'x')
end

# The kernel and the two rootfs sizes one board is given.
def members(board)
  [synthetic("kernel:#{board}", SIZES['kernel']),
   { 'lite' => synthetic("rootfs:#{board}", SIZES['rootfs']), 'big' => synthetic("big:#{board}", TOO_LARGE) }]
end

# Synthetic release assets for one SoC, written into the mirror; returns
# name => bytes.
def synthetic_assets(soc, mirror)
  board = soc.board
  kernel, roots = members(board)
  assets = { soc.uboot_filename => synthetic("uboot:#{board}", SIZES['uboot']) }
  roots.to_a.product(%w[nor nand]).each do |(release, root), flash|
    assets["openipc.#{board}-#{flash}-#{release}.tgz"] = linux_tarball(board, flash, kernel, root)
  end
  assets.each { |name, data| File.binwrite(File.join(mirror, name), data) }
end

# Every flash layout a SoC can be asked for, and the two that do not fit.
def cases_for(soc)
  [[8, nil], [16, nil], [16, 8], [32, nil], [32, 8]].map { |size, layout| [soc, 'nor', 'lite', size, layout] } +
    [[soc, 'nand', 'lite', nil, nil], [soc, 'nor', 'big', 8, nil], [soc, 'nor', 'big', 16, nil]]
end

# Where each synthetic member landed in an image.
def offsets(image, board, release)
  { 'u-boot' => "uboot:#{board}", 'kernel' => "kernel:#{board}",
    'rootfs' => "#{release == 'big' ? 'big' : 'rootfs'}:#{board}" }
    .transform_values { |seed| image.index(synthetic(seed, 64)) }
end

def manifest(soc, flash, release, size, layout)
  row = { urlname: soc.urlname, vendor: soc.vendor.name, flash_type: flash, release: release,
          size: size, layout: layout }
  fw = Firmware.new(size: size, flash_type: flash, release: release, soc: soc, layout: layout)
  fw.generate
  image = File.binread(fw.filepath)
  row.merge(filename: fw.filename, bytes: image.bytesize, sha256: Digest::SHA256.hexdigest(image),
            offsets: offsets(image, soc.board, release))
rescue Firmware::PayloadTooLarge => e
  row.merge(error: 'too_large', message: e.message)
end

def index_for(assets)
  {
    'generated_at' => INDEX['generated_at'], 'aliases' => INDEX['aliases'],
    'assets' => assets.transform_values do |data|
      { 'size' => data.bytesize, 'digest' => "sha256:#{Digest::SHA256.hexdigest(data)}",
        'updated_at' => '2026-09-01T00:00:00Z', 'release' => 'latest' }
    end
  }
end

manifests = Dir.mktmpdir do |dir|
  mirror, index_root, cache = %w[mirror index files].map { |d| File.join(dir, d).tap { |p| FileUtils.mkdir_p(p) } }
  ENV['RELEASE_MIRROR_ROOT'] = mirror
  ENV['RELEASE_INDEX_ROOT'] = index_root
  Firmware.cache_dir = cache

  # One SoC per vendor that has a bootloader: the layout table is chosen by
  # vendor, and this is every vendor the catalogue can build for.
  representatives = Soc.all.group_by { |s| s.vendor.name }.filter_map do |_, socs|
    socs.sort_by(&:model).find { |s| s.uboot_filename.present? }
  end
  assets = representatives.map { |soc| synthetic_assets(soc, mirror) }.reduce({}, :merge)
  File.write(File.join(index_root, '.index.json'), JSON.generate(index_for(assets)))
  ReleaseIndex.reset!

  representatives.flat_map { |soc| cases_for(soc) }.map { |args| manifest(*args) }
end

File.write(OUT.join('availability.json'), JSON.generate(availability))
File.write(OUT.join('boards.json'), "#{JSON.pretty_generate(boards)}\n")
File.write(OUT.join('manifests.json'), "#{JSON.pretty_generate(manifests)}\n")
puts "#{boards.size} boards, #{manifests.size} manifests (#{manifests.count { |m| m[:error] }} refusals)"
