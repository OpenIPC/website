# frozen_string_literal: true

class BinariesController < ApplicationController

  FIRMWARE_ROOT = '/srv/github-releases'
  
  def index
#    @binaries = Dir.glob("#{FIRMWARE_ROOT}/openipc.*.tgz").map do |b|
    @binaries = File.readlines("#{FIRMWARE_ROOT}/.rootfs.sizes").map do |line|
      #filesize = `tar -tvf #{b} | grep squashfs | grep -v md5sum | awk '{print $3}'`.to_i
      filename = line.split(" ").first
      filesize = line.split(" ").second.to_i
      filesize_limit = size_limit(filename)
      freespace = filesize_limit - filesize
      {
        name: filename,
        size: filesize,
        limit: filesize_limit,
        free: freespace 
      }
    end
    render 'binaries/index'
  end

  def size_limit(name)
    if name.ends_with?('-lite.tgz') || name.ends_with?('-mini.tgz') ||
        name.ends_with?('gk7205v200-nor-fpv.tgz') ||
        name.ends_with?('gk7205v300-nor-fpv.tgz')
      5.megabytes
    elsif name.ends_with?('-ultimate.tgz') || name.ends_with?('-fpv.tgz')
      10.megabytes
    else
      0
    end
  end
end
