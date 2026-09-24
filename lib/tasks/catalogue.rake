# frozen_string_literal: true

# The catalogue lives in data/catalogue/*.yml (#161) and the hardware pages are
# built from it (#162). While the socs and vendors tables are still here, the
# two can drift -- somebody edits a SoC in the admin and the pages keep
# rendering last month's, with nothing to say so.
#
# There is no test for that: the test database carries fixtures, not the
# catalogue, so a comparison there would be against an empty database. This is
# the comparison, to be run where the real rows are.
#
#   bin/rails catalogue:diff     what the database has that the files do not
#   bin/rails catalogue:export   rewrite the files from the database
#
# catalogue_bake.rake's `catalogue:check` does the same job for the two figures
# the home page bakes, and says in its own comment that #161 ends the need for
# it. It does: once the pages read these files, the figures are a function of
# the tree the build already reads.
namespace :catalogue do
  def catalogue_columns
    Soc.column_names - %w[id vendor_id created_at updated_at]
  end

  def catalogue_dir
    Rails.root.join('data/catalogue')
  end

  def vendor_document(vendor)
    {
      'name' => vendor.name, 'urlname' => vendor.urlname,
      'full_name' => vendor.full_name, 'website_url' => vendor.website_url,
      'notes' => vendor.notes,
      'socs' => vendor.socs.order(:model).map do |soc|
        catalogue_columns.to_h { |column| [column, soc.public_send(column)] }
      end,
    }
  end

  desc 'Report where data/catalogue and the database disagree'
  task diff: :environment do
    differences = 0

    Vendor.order(:urlname).find_each do |vendor|
      file = catalogue_dir.join("#{vendor.urlname}.yml")
      unless File.exist?(file)
        puts "missing file: #{vendor.urlname}.yml (#{vendor.socs.count} SoCs)"
        differences += 1
        next
      end

      on_disk = YAML.safe_load_file(file)
      wanted = vendor_document(vendor)
      next if on_disk == wanted

      (wanted.keys - ['socs']).each do |key|
        next if on_disk[key] == wanted[key]

        puts "#{vendor.urlname}: #{key}: file #{on_disk[key].inspect} / db #{wanted[key].inspect}"
        differences += 1
      end

      by_model = on_disk['socs'].index_by { |soc| soc['model'] }
      wanted['socs'].each do |soc|
        found = by_model.delete(soc['model'])
        if found.nil?
          puts "#{vendor.urlname}: #{soc['model']} is in the database and not in the file"
          differences += 1
          next
        end

        soc.each do |column, value|
          next if found[column] == value

          puts "#{vendor.urlname}: #{soc['model']}.#{column}: " \
               "file #{found[column].inspect} / db #{value.inspect}"
          differences += 1
        end
      end

      by_model.each_key do |model|
        puts "#{vendor.urlname}: #{model} is in the file and not in the database"
        differences += 1
      end
    end

    Dir[catalogue_dir.join('*.yml')].each do |file|
      urlname = YAML.safe_load_file(file)['urlname']
      next if Vendor.exists?(urlname: urlname)

      puts "#{urlname} is in the files and not in the database"
      differences += 1
    end

    if differences.zero?
      puts 'data/catalogue matches the database'
    else
      puts "#{differences} difference(s)"
      exit 1
    end
  end

  desc 'Bake data/catalogue into the JSON the frontend build reads'
  task bake: :environment do
    require 'catalogue_export'
    CatalogueExport.write
    puts "wrote #{CatalogueExport::OUT}"
  end

  desc 'Rewrite data/catalogue from the database'
  task export: :environment do
    FileUtils.mkdir_p(catalogue_dir)

    # A vendor that has been removed or renamed leaves its file behind, and
    # CatalogueExport globs every file it finds -- so the next bake would put
    # the departed vendor back on the pages. Written first so a failure part
    # way through leaves the tree short rather than stale.
    wanted = Vendor.pluck(:urlname).map { |urlname| "#{urlname}.yml" }
    Dir[catalogue_dir.join('*.yml')].each do |file|
      next if wanted.include?(File.basename(file))

      puts "removing #{File.basename(file)}: no such vendor"
      File.delete(file)
    end

    Vendor.order(:name).find_each do |vendor|
      unless vendor.urlname.match?(Vendor::URLNAME_FORMAT)
        abort "refusing to write #{vendor.urlname.inspect}: not a safe slug"
      end

      document = vendor_document(vendor)
      File.open(catalogue_dir.join("#{vendor.urlname}.yml"), 'w') do |file|
        file.puts "# #{vendor.name} — #{document['socs'].size} SoC(s)."
        file.puts '#'
        file.puts '# Exported from the catalogue table (#161). This file is the source of'
        file.puts '# truth for the hardware pages: edit it in a pull request, not in the'
        file.puts '# admin, and the prerendered pages follow on the next build.'
        file.write(document.to_yaml.delete_prefix("---\n"))
      end
    end
    puts "wrote #{Vendor.count} file(s) to data/catalogue"
  end
end

# The wizard's own export (#163). Separate namespace: it enumerates ~40
# combinations per SoC and renders each one's command lines, so it costs
# considerably more than baking the catalogue and is not something to run on
# every build by accident.
namespace :wizard do
  desc "Export every combination's command blocks, one file per SoC"
  task export: :environment do
    require 'wizard_export'
    written = WizardExport.write_all
    total = written.sum { |(_, count)| count }
    puts "wrote #{written.size} file(s), #{total} combination(s) to #{WizardExport::OUT_DIR}"
  end
end
