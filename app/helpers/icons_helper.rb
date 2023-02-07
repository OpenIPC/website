module IconsHelper
  def icon_download
    image_tag "download.svg", alt: "Image: download icon",
      class: "icon img-fluid", title: "Download"
  end

  def icon_instruction
    image_tag "instruction.svg", alt: "Image: information icon",
      class: "icon img-fluid", title: "Installation instruction"
  end

  def icon_language
    image_tag "translate.svg", alt: "Image: language icon",
      class: "icon img-fluid", title: "Language selection"
  end

  def icon_nothing
    image_tag "square-line.svg", alt: "Image: empty icon",
      class: "icon img-fluid"
  end
end
