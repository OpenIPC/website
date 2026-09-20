# frozen_string_literal: true

# How many firmware images one address has made us assemble lately.
#
# `download_full_image` is the most expensive request on the site -- a cache
# miss reads two release tarballs and writes an 8-32MB image -- and the nginx
# limits in deploy/nginx/conf.d/openipc-firmware-rate.conf can only count
# requests, not builds. They cannot tell a cache miss from a cache hit, and a
# 16-32MB image legitimately arrives as thirty range requests in a minute, so
# no request-rate low enough to mean "six builds" is high enough to let a real
# download finish. This is the half that counts builds, where the cache miss is
# actually known (#147).
#
# Measured before the numbers were chosen: over the fifteen days to 2026-09-20,
# no address requested more than THREE distinct firmware URLs in one minute,
# and 96% of minutes carried one. Six is twice the observed maximum.
class FirmwareBuild < ApplicationRecord
  WINDOW = 1.minute
  LIMIT = 6

  # Long enough that a stuck clock or a burst cannot erase the evidence before
  # WINDOW closes, short enough that the table never becomes something anyone
  # has to think about. At ~113 builds a day it holds a handful of rows.
  RETENTION = 1.hour

  def self.over_limit?(ip_address)
    return false if ip_address.blank?

    where(ip_address: ip_address, created_at: WINDOW.ago..).count >= LIMIT
  end

  # Never raises. A failure to record makes the limit leaky for a minute, which
  # is a far better outcome than failing a request that has already produced a
  # valid firmware image -- the same reasoning as Download.record.
  def self.record(ip_address)
    return if ip_address.blank?

    create!(ip_address: ip_address, created_at: Time.current)
    prune
  rescue StandardError => e
    Rails.logger.error "firmware build not recorded for #{ip_address}: #{e.class}: #{e.message}"
    nil
  end

  # Here rather than in a cron job, because the table is only ever written on
  # the expensive path -- about a hundred times a day -- so one indexed DELETE
  # alongside each build costs nothing and removes a moving part.
  def self.prune
    where(created_at: ...RETENTION.ago).delete_all
  end
end
