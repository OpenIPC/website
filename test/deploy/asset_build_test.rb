# frozen_string_literal: true

require 'json'
require 'test_helper'

# The build flags are four strings in package.json that no other test touches,
# and each was worth 100KB or more on the wire (#149). Dropping one would make
# the site slower for every visitor and break nothing.
class AssetBuildTest < ActiveSupport::TestCase
  SCRIPTS = JSON.parse(Rails.root.join('package.json').read).fetch('scripts').freeze

  test 'the production JavaScript bundle is minified' do
    assert_includes SCRIPTS.fetch('build'), '--minify',
                    'unminified, application.js is 399KB against 183KB'
  end

  # 685KB of .map was being precompiled and shipped to production.
  test 'the production build emits no sourcemap' do
    assert_not_includes SCRIPTS.fetch('build'), '--sourcemap',
                        'a production sourcemap is precompiled and served to visitors'
  end

  # The trade runs the other way in development, where bytes do not matter and
  # a minified bundle with no map is a debugging wall. Procfile.dev runs this.
  test 'development keeps its sourcemaps' do
    assert_includes SCRIPTS.fetch('build:watch'), '--sourcemap'
    assert_not_includes SCRIPTS.fetch('build:watch'), '--minify'
    assert_includes Rails.root.join('Procfile.dev').read, 'yarn build:watch',
                    'bin/dev must run the build that keeps sourcemaps'
  end

  test 'the stylesheet is compressed' do
    assert_includes SCRIPTS.fetch('build:css:compile'), '--style=compressed',
                    'expanded, application.css is 1.2MB'
  end

  # This one is the whole reason the CSS was 1.2MB rather than 338KB: sass was
  # already told --no-source-map, and then postcss appended a 625,688-byte
  # base64 sourcemap of its own, which shipped to every visitor.
  test 'postcss does not append a sourcemap of its own' do
    assert_includes SCRIPTS.fetch('build:css:prefix'), '--no-map',
                    'postcss inlines a base64 sourcemap by default -- 625KB of it here'
  end
end
