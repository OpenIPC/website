# frozen_string_literal: true

class PagesController < ApplicationController
  def show
  end

  def about
    @page_title = t('pages.about_us.title')
    render 'pages/our_channels'
  end

  def firmware_partitions_calculation
    @page_title = t('pages.firmware_partitions_calculation.title')
    render 'pages/firmware_partitions_calculation'
  end

  def high_resolution_timer
    @page_title = t('pages.high_resolution_timer.title')
    render 'pages/high_resolution_timer'
  end

  def introduction
    @page_title = t('pages.introduction.title')
    render 'pages/introduction'
  end

  def majestic_endpoints
    @page_title = t('pages.majestic_endpoints.title')
    render 'pages/majestic_endpoints'
  end

  def our_channels
    @page_title = t('pages.our_channels.title')
    render 'pages/our_channels'
  end

  def our_projects
    @page_title = t('pages.our_projects.title')
    render 'pages/our_projects'
  end

  def our_team
    @page_title = t('pages.our_team.title')
    render 'pages/our_team'
  end

  def stages_of_firmware_development
    @page_title = t('pages.stages_of_firmware_development.title')
    render 'pages/stages_of_firmware_development'
  end

  def support_open_source
    @page_title = t('pages.support_open_source.title')
    render 'pages/support_open_source'
  end
end
