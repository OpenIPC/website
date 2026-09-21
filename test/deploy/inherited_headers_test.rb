# frozen_string_literal: true

require 'test_helper'

# `add_header` in a location REPLACES every inherited one rather than adding to
# it. So a location that sets a cache header, or a witness like X-Served-By,
# silently drops whatever the server block was sending -- and because the thing
# most often dropped is HSTS, which a browser has already cached from every
# other page on the site, nothing looks wrong.
#
# `page_cache_test.rb` has asserted this since #233, for one location. It was
# written after exactly this mistake and it only ever looked at the catch-all,
# which is how /wall/, /assets|fonts/, the two gallery caches and the support
# stats file came to serve every image, stylesheet and font on openipc.org
# without HSTS. This file asks the question of every location instead.
class InheritedHeadersTest < ActiveSupport::TestCase
  VHOSTS = %w[org.openipc org.openipc.dev].freeze

  # A location and its body, matched by its closing brace sitting at the same
  # indentation as its opening -- which is true of every location in these
  # files at either of the two indentations they use, and lets a nested
  # `if (...) { ... }` pass through as part of the body rather than ending it.
  LOCATION = /^(?<indent>[ \t]*)(?<header>location [^\n{]*\{)\n(?<body>(?:.*\n)*?)\k<indent>\}$/

  # A server-level directive is one at the outermost indentation inside a
  # `server {` block: four spaces in these files, never more.
  SERVER_HEADER = /^ {4}(add_header .*;)$/

  VHOSTS.each do |name|
    test "#{name}: no location drops a header the server block sends" do
      text = Rails.root.join("deploy/nginx/sites-available/#{name}").read
      inherited = text.scan(SERVER_HEADER).flatten.uniq
      locations = text.scan(LOCATION).map { |_indent, header, body| [header, body] }

      refute_empty inherited, "no server block in #{name} declares an add_header; this test reads nothing"
      refute_empty locations, "found no locations in #{name}; this test reads nothing"

      dropping = locations.select { |_header, body| body.match?(/^\s*add_header /) }
                          .flat_map do |header, body|
                            inherited.reject { |h| body.include?(h) }.map { |h| [header, h] }
                          end

      assert_empty dropping, <<~MESSAGE.chomp
        These locations use `add_header` and do not repeat one the server block
        sends, so everything they serve goes out without it:

        #{dropping.map { |header, h| "  #{header}\n    missing: #{h}" }.join("\n")}

        add_header at location level replaces the whole inherited set. Repeat
        every server-level add_header in any location that declares one of its
        own -- there is no syntax for "and also keep the others".
      MESSAGE
    end
  end
end
