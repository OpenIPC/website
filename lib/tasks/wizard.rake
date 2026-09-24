# frozen_string_literal: true

# The wizard's export, and the check that it is the same thing Rails renders
# (#163, #164).
#
# Both tasks need the release index, which is a file the publisher refreshes on
# the host -- so they run where it is, not in CI. `Soc::RELEASES_ROOT` can be
# pointed at a copy for a local run.
namespace :wizard do
  desc "Export every combination's command blocks, one file per SoC"
  task export: :environment do
    require 'wizard_export'
    written = WizardExport.write_all
    total = written.sum { |(_, count)| count }
    puts "wrote #{written.size} file(s), #{total} combination(s) to #{WizardExport::OUT_DIR}"
  end

  # #164 does not ship until this is empty.
  #
  # The export exists so that nobody re-derives flash geometry in JavaScript:
  # Rails renders its own command lines and the frontend fills three holes. The
  # whole claim rests on the export saying exactly what the page says, for
  # every combination the menu can reach -- which is 2,940 of them across 126
  # SoCs, far more than anyone will read.
  #
  # So it is checked rather than reviewed. For each combination this rebuilds
  # the camera the page would have built, asks the helper for its lines, and
  # compares them with the exported ones after the holes are filled back in.
  desc 'Diff every exported combination against what Rails renders'
  task verify: :environment do
    require 'wizard_export'

    view = ApplicationController.new
                                .tap { |c| c.request = ActionDispatch::TestRequest.create }
                                .view_context

    checked = 0
    differences = 0
    skipped = 0

    Soc.includes(:vendor).find_each do |soc|
      document = WizardExport.document(soc)

      document['combinations'].each do |entry|
        if entry['page']
          skipped += 1
          next
        end

        camera = Camera.new(
          camera_ip_address: WizardExport::IPADDR,
          server_ip_address: WizardExport::SERVERIP,
          camera_mac_address: WizardExport::ETHADDR,
          flash_type: entry['flash_type'],
          firmware_version: entry['edition'],
          network_interface: entry['network_interface'],
          sd_card_slot: entry['sd_card_slot']
        )
        camera.partition_layout = entry['partition_layout'] if entry['partition_layout']
        camera.soc = soc

        WizardExport::BLOCKS.each do |name|
          rendered = view.public_send("#{name}_lines", camera)
                         .map(&:to_s)
                         .reject { |line| line.start_with?('<') }
          exported = document['blocks'].fetch(entry['blocks'].fetch(name))['lines']
          checked += 1
          next if rendered == exported

          differences += 1
          puts "#{soc.urlname} #{entry['flash_type']}/#{entry['partition_layout']}/" \
               "#{entry['edition']}/#{entry['network_interface']}/#{entry['sd_card_slot']} #{name}:"
          (rendered - exported).each { |line| puts "  rails only: #{line}" }
          (exported - rendered).each { |line| puts "  export only: #{line}" }
        end
      end
    end

    puts "#{checked} block(s) compared, #{skipped} combination(s) with a page of their own"
    if differences.zero?
      puts 'the export says exactly what Rails renders'
    else
      puts "#{differences} block(s) differ"
      exit 1
    end
  end
end
