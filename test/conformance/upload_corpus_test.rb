# frozen_string_literal: true

require_relative 'conformance_helper'

# Rails' verdicts, replayed (#291).
#
# fixtures/content_types.json and fixtures/mac_addresses.json were written by
# Rails (`bin/rails conformance:fixtures`): for each input, what the model said.
# These send each input over HTTP and require the same answer. Nothing here
# restates the rules, because the rules are not ours -- which bytes are an image
# is Marcel's table, and the MAC pattern is a regex a port will want to tidy.
#
# Nothing is stored: every file verdict is read from a refusal, sent with a MAC
# the server refuses, and every MAC verdict from an upload with no file.
class UploadCorpusConformanceTest < Conformance::Case
  def mismatches_report(mismatches, total)
    "#{mismatches.size} of #{total} disagree with Rails:\n" +
      mismatches.first(20).map { |m| "  #{m}" }.join("\n")
  end

  test 'every file is judged the way Rails judges it' do
    corpus = Conformance.fixture('content_types')
    prefixes = corpus['prefixes'].transform_values { |hex| [hex].pack('H*') }

    mismatches = corpus['cases'].filter_map do |entry|
      file = { filename: entry['filename'], content_type: entry['declared'],
               data: Conformance::Images.padded(prefixes.fetch(entry['prefix']), corpus['size']) }
      response = upload(mac: corpus['probe_mac'], file: file)
      expected = (entry['file_errors'] + corpus['mac_error']).join('. ')
      next if response.code == '415' && response['X-Error'] == expected

      "#{entry['prefix']} as #{entry['declared'].inspect} named #{entry['filename']}: " \
        "want #{expected.inspect}, got #{response.code} #{response['X-Error'].inspect}"
    end

    assert_empty mismatches, mismatches_report(mismatches, corpus['cases'].size)
  end

  test 'every MAC spelling is judged the way Rails judges it' do
    corpus = Conformance.fixture('mac_addresses')

    mismatches = corpus['cases'].filter_map do |entry|
      response = upload(mac: entry['mac'], file: false)
      expected = (["File can't be blank"] + entry['mac_errors']).join('. ')
      next if response.code == '415' && response['X-Error'] == expected

      "#{entry['mac'].inspect}: want #{expected.inspect}, got #{response.code} #{response['X-Error'].inspect}"
    end

    assert_empty mismatches, mismatches_report(mismatches, corpus['cases'].size)
  end
end
