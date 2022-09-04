class PagesController < ApplicationController
  def show
  end

  def about
    @page_title = "About Us"
    render "pages/our-channels"
  end

  def firmware_partitions_calculation
    @page_title = "Firmware partitions calculation"
    render "pages/firmware-partitions-calculation"
  end

  def introduction
    @page_title = "Introduction"
    render "pages/introduction"
  end

  def majestic_endpoints
    @page_title = "Majestic Endpoints"
    render "pages/majestic-endpoints"
  end

  def our_channels
    @page_title = "Our channels"
    render "pages/our-channels"
  end

  def our_projects
    @page_title = "Our Projects"
    render "pages/our-projects"
  end

  def our_team
    @page_title = "Team"
    render "pages/our-team"
  end

  def stages_of_firmware_development
    @page_title = "Stages of Firmware development"
    render "pages/stages-of-firmware-development"
  end

  def support_open_source
    @page_title = "Support Open Source"
    render "pages/support-open-source"
  end
end
