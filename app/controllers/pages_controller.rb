# frozen_string_literal: true

class PagesController < ApplicationController
  def show
  end

  def business
    @page_title = t('pages.business.title')
    render 'pages/business'
  end

  def community
    @page_title = t('pages.community.title')
    render 'pages/community'
  end

  def donate
    @page_title = t('pages.donate.title')
    render 'pages/donate'
  end

  def ecosystem
    @page_title = t('pages.ecosystem.title')
    render 'pages/ecosystem'
  end

  def firmware_partitions_calculation
    @page_title = t('pages.firmware_partitions_calculation.title')
    render 'pages/firmware_partitions_calculation'
  end

  def get_started
    @page_title = t('pages.get_started.title')
    render 'pages/get_started'
  end

  def green_life
    @page_title = t('pages.green_life.title')
    render 'pages/green_life'
  end

  def high_resolution_timer
    @page_title = t('pages.high_resolution_timer.title')
    render 'pages/high_resolution_timer'
  end

  # The homepage the relaunch is building towards. Reachable by URL, but the
  # root route and the navigation still point at #introduction until the cutover.
  #
  # The counts are read rather than written into the copy so they cannot go
  # stale, and the page renders with all of them at zero -- a fresh checkout has
  # an empty database and must not 500.
  def home
    @page_title = t('pages.home.title')
    @meta_description = t('site.default_meta_description')
    @wall_snapshots = Snapshot.latest_per_camera(limit: 5)
    @soc_count = Soc.count
    @vendor_names = Vendor.order(:name).pluck(:name)
    render 'pages/home'
  end

  def low_latency
    @page_title = t('pages.low_latency.title')
    render 'pages/low_latency'
  end

  def majestic_endpoints
    @page_title = t('pages.majestic_endpoints.title')
    render 'pages/majestic_endpoints'
  end

  def merchandise
    @page_title = "OpenIPC Merchandise"
    render 'pages/merchandise'
  end

  def our_team
    @page_title = t('pages.our_team.title')
    render 'pages/our_team'
  end

  def qr_code_generator
    @page_title = t('pages.qr_code_generator.title')
    render 'pages/qr_code_generator'
  end

  def stages_of_firmware_development
    @page_title = t('pages.stages_of_firmware_development.title')
    render 'pages/stages_of_firmware_development'
  end

  def utilities
    @page_title = t('pages.utilities.title')
    render 'pages/utilities'
  end

  def web_interface
    @page_title = t('pages.web_interface.title')
    render 'pages/web_interface'
  end
end
