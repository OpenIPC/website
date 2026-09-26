# frozen_string_literal: true

# Writes the golden the Go wizard export is held to (#300), from the Ruby
# implementation it replaces. Run it with the Rails app, not in it:
#
#   bin/rails runner service/testdata/gen/wizard_golden.rb
#
# It runs WizardExport.write_all against the committed snapshot of the release
# index (service/internal/firmware/testdata/release-index.json) and records the
# SHA-256 of every file it wrote, plus one over the sorted concatenation, in
# service/internal/wizard/testdata/digests.json. The Go test rebuilds every
# file from the same catalogue and index and must reproduce every digest: the
# whole export, 126 files and every combination in them, special pages
# included, compared as bytes.

require 'digest'
require 'json'
require 'tmpdir'
require Rails.root.join('lib/wizard_export').to_s

INDEX = Rails.root.join('service/internal/firmware/testdata/release-index.json')
OUT = Rails.root.join('service/internal/wizard/testdata/digests.json')

def special_pages(bodies)
  bodies.sum { |body| JSON.parse(body)['combinations'].count { |c| c['page'] } }
end

Dir.mktmpdir do |dir|
  index_root = File.join(dir, 'index')
  export = File.join(dir, 'export')
  FileUtils.mkdir_p(index_root)
  FileUtils.cp(INDEX, File.join(index_root, '.index.json'))
  ENV['RELEASE_INDEX_ROOT'] = index_root
  ReleaseIndex.reset!

  written = WizardExport.write_all(dir: export)
  names = Dir[File.join(export, '*.json')].map { |f| File.basename(f) }.sort
  bodies = names.map { |name| File.binread(File.join(export, name)) }
  golden = {
    'note' => 'Written by service/testdata/gen/wizard_golden.rb from lib/wizard_export.rb.',
    'combinations' => written.sum { |(_, n)| n },
    'special_page_combinations' => special_pages(bodies),
    'all' => Digest::SHA256.hexdigest(bodies.join),
    'files' => names.zip(bodies).to_h { |name, body| [name, Digest::SHA256.hexdigest(body)] }
  }
  File.write(OUT, "#{JSON.pretty_generate(golden)}\n")
  puts "#{names.size} files, #{golden['combinations']} combinations " \
       "(#{golden['special_page_combinations']} special-page), all #{golden['all'][0, 16]}"
end
