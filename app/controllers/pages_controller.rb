class PagesController < ApplicationController
  def show
  end

  def about
    @page_title = "About Us"
    render "pages/about"
  end

  def firmware_partitions_calculation
    @page_title = "Firmware partitions calculation"
    render "pages/firmware-partitions-calculation"
  end

  def introduction
    @page_title = "Introduction"
    render "pages/introduction"
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
