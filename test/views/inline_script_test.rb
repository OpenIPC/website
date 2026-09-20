# frozen_string_literal: true

require 'test_helper'

# Two rules about <script> blocks written into a view, both of which cost us a
# broken page when Turbo was switched on in #150.
#
# Turbo swaps the <body> instead of reloading the document. That has one
# consequence in each direction: the scripts in the new body are executed
# again, and the document they execute in is the old one, with everything the
# previous page left behind still running in it.
class InlineScriptTest < ActiveSupport::TestCase
  VIEWS = Rails.root.glob('app/views/**/*.erb').freeze

  # An opening <script> carrying no attributes. A `<script src=...>` include
  # gets a fresh scope per fetch and is not what this is about.
  BARE_SCRIPT = /^[ \t]*<script>[ \t]*$/

  test 'every inline script in a view is wrapped in a scope' do
    offenders = VIEWS.flat_map { |path| unscoped_scripts(path) }

    assert_empty offenders, <<~MESSAGE.chomp
      These inline scripts run in the global lexical scope:

      #{offenders.map { |o| "  #{o}" }.join("\n")}

      A classic script's top-level `const`, `let` and `class` go into a scope
      that belongs to the document rather than to the page -- and Turbo keeps
      the document across a navigation while executing the new body's scripts
      again. The second visit to such a page throws "Identifier 'x' has
      already been declared" before anything else in the block runs, so the
      page arrives with no listeners at all.

      Wrap the body in `(() => { ... })();`.
    MESSAGE
  end

  test 'no view waits for a load event that Turbo never fires' do
    offenders = VIEWS.flat_map do |path|
      File.readlines(path).each_with_index.filter_map do |line, i|
        next unless line.match?(/addEventListener\(\s*['"]load['"]|window\.onload/)

        "#{path.relative_path_from(Rails.root)}:#{i + 1}"
      end
    end

    assert_empty offenders, <<~MESSAGE.chomp
      These views wait for a `load` event:

      #{offenders.map { |o| "  #{o}" }.join("\n")}

      `load` fires for the document the browser fetched and never again, so
      under Turbo it fires only for whichever page the visitor typed the URL
      of; every page reached by clicking a link gets nothing. Call the
      initialiser directly instead -- an inline script placed below its own
      markup already runs at the right moment, on the first load and on every
      visit after it.
    MESSAGE
  end

  private

  # The first line of real code inside each bare <script>, and whether it opens
  # a scope of its own.
  def unscoped_scripts(path)
    lines = File.readlines(path, chomp: true)
    openers(lines).filter_map do |i|
      first_code = first_statement(lines, i)
      next if first_code.nil? || scope_opener?(first_code)

      "#{path.relative_path_from(Rails.root)}:#{i + 1} starts with #{first_code[0, 60].inspect}"
    end
  end

  def openers(lines)
    lines.each_index.select { |i| lines[i].match?(BARE_SCRIPT) }
  end

  def first_statement(lines, opener)
    lines[(opener + 1)..]
      .map(&:strip)
      .find { |l| !l.empty? && !l.start_with?('//', '/*', '*', '<%#') }
  end

  def scope_opener?(line)
    line.match?(/\A\(\s*(\(\s*\)\s*=>|function\b)/)
  end
end
