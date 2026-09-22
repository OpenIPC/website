# frozen_string_literal: true

require 'open3'
require 'tmpdir'
require 'yaml'
require 'test_helper'

# The bundle nginx serves in front of Rails (#157).
#
# The seam cannot fail closed -- try_files walks past every miss and ends at
# @rails -- so a bundle that is absent, empty or unreadable means Rails answers
# everything, which is where everything is answered today. What a bundle CAN do
# is take over an address that is not its to take, silently and with nothing in
# the Rails log. That is what deploy/static/check-bundle.sh is for, and what
# most of this file is about.
class StaticBundleTest < ActiveSupport::TestCase
  STATIC = Rails.root.join('deploy/static')
  BUILD = STATIC.join('build.sh')
  CHECK = STATIC.join('check-bundle.sh')
  RESERVED = STATIC.join('reserved-paths').read.lines
                   .map(&:strip).reject { |l| l.empty? || l.start_with?('#') }.freeze

  # A stand-in for the Astro build (#159).
  #
  # These tests are about build.sh's machinery -- the revision, the manifest,
  # the refusals -- and none of it cares what the pages say. Building the real
  # site here would put Node on the critical path of `bin/rails test`, which
  # is a Ruby suite and should stay one. deploy/static/build.sh is exercised
  # against the real Astro output in the `build` job, where Node already is.
  #
  # The shapes are the ones Astro actually emits: the smoke page in three
  # locale trees, an asset directory, and the @@TOKEN@@s build.sh substitutes.
  FIXTURE_REVISION = '0123456789abcdef0123456789abcdef01234567'

  def fixture_site(dir)
    FileUtils.mkdir_p("#{dir}/_smoke")
    File.write("#{dir}/_smoke/index.html", <<~HTML)
      <!doctype html><html lang="en"><body>
      <p>built from @@REVISION@@ at @@BUILT@@</p>
      </body></html>
    HTML

    %w[ru zh].each do |locale|
      FileUtils.mkdir_p("#{dir}/#{locale}/_smoke")
      File.write("#{dir}/#{locale}/_smoke/index.html", <<~HTML)
        <!doctype html><html lang="#{locale}"><body>
        <p>built from @@REVISION@@ at @@BUILT@@</p>
        </body></html>
      HTML
    end

    FileUtils.mkdir_p("#{dir}/_astro")
    File.write("#{dir}/_astro/app.css", 'body{}')
    dir
  end

  def build(dir, site_dist: nil)
    # A fixed commit rather than this checkout's. build.sh only insists the
    # revision is forty hex characters, and asking git for the real one means
    # the suite needs git installed and a .git it can resolve -- which a git
    # worktree, whose .git is a file pointing elsewhere, does not always give.
    env = { 'STATIC_SITE_DIST' => site_dist.to_s, 'GITHUB_SHA' => FIXTURE_REVISION }
    out, status = Open3.capture2e(env, 'bash', BUILD.to_s, dir)
    [out, status]
  end

  def check(site, manifest = nil)
    Open3.capture2e('bash', CHECK.to_s, site.to_s, *[manifest&.to_s].compact)
  end

  # A bundle shaped like the one CI produces.
  def with_bundle
    Dir.mktmpdir do |tmp|
      site_dist = fixture_site(File.join(tmp, 'site-src'))
      out, status = build(File.join(tmp, 'dist'), site_dist: site_dist)
      assert status.success?, "build.sh failed:\n#{out}"
      yield File.join(tmp, 'dist')
    end
  end

  test 'the build produces a served tree and its sidecars' do
    with_bundle do |dist|
      assert File.file?("#{dist}/site/_smoke/index.html")
      assert File.file?("#{dist}/MANIFEST")
      assert File.file?("#{dist}/REVISION")
    end
  end

  # MANIFEST beside the served tree, not inside it. Inside, it would be
  # fetchable at https://openipc.org/MANIFEST the day the bundle holds the
  # marketing pages.
  test 'the sidecars are not themselves served' do
    with_bundle do |dist|
      assert_not File.exist?("#{dist}/site/MANIFEST")
      assert_not File.exist?("#{dist}/site/REVISION")
    end
  end

  # This is what makes a rollback visible from outside. `readlink` on the host
  # says which bundle is linked; the smoke page says which one is being served,
  # which is the question actually being asked.
  test 'the smoke page names the commit it was built from' do
    with_bundle do |dist|
      page = File.read("#{dist}/site/_smoke/index.html")

      assert_match(/[0-9a-f]{40}/, page, 'the smoke page does not name a commit')
      assert_includes page, File.read("#{dist}/REVISION").strip
      assert_not_includes page, '@@', 'an unsubstituted @@TOKEN@@ shipped in the bundle'
    end
  end

  # The home page is the one file whose presence changes what the site means.
  #
  # Rails renders `/` per Accept-Language and declares `Vary: Accept-Language`;
  # a file cannot vary, and try_files never sees the query string either, so
  # ?locale=ru would stop redirecting as well. Neither is a reason not to
  # extract it -- both are reasons it is a decision. #160 is where it is made,
  # and deleting this test is part of making it.
  test 'the home page is not extracted yet' do
    with_bundle do |dist|
      assert_not File.exist?("#{dist}/site/index.html"), <<~MESSAGE.chomp
        The bundle has a root index.html, so nginx now answers `/` from a file.

        That silently ends Accept-Language negotiation on the bare path and
        the ?locale= redirect with it. If this is deliberate, it belongs in
        #160 together with what `/` is supposed to mean -- not in a build.
      MESSAGE
    end
  end

  test 'the bundle is readable by a worker that is not root' do
    with_bundle do |dist|
      Dir.glob("#{dist}/site/**/*", File::FNM_DOTMATCH).each do |path|
        next if File.basename(path).start_with?('.')

        mode = File.stat(path).mode
        wanted = File.directory?(path) ? 0o001 : 0o004
        assert_equal wanted, mode & wanted,
                     "#{path} is mode #{format('%o', mode & 0o777)}; nginx cannot read it"
      end
    end
  end

  test 'a clean bundle passes its own checks' do
    with_bundle do |dist|
      out, status = check("#{dist}/site")
      assert status.success?, "check-bundle.sh refused a bundle build.sh produced:\n#{out}"
    end
  end

  # CI calls this as `check-bundle.sh dist/site` from the repository root, and
  # rule 8 verifies the manifest from inside the tree -- so a relative path that
  # is not resolved first stops existing the moment it cds. Every test above
  # passes an absolute tmpdir and none of them would have caught it; CI did.
  # --- the shapes #159 brought -------------------------------------------
  #
  # Rule 2 used to demand an index.html in every directory. A bundle with more
  # than one page cannot satisfy that: `ru/` sits above `ru/donate/index.html`
  # and is not itself a page, and the build writes `_astro/` of stylesheets
  # and fonts. deploy/nginx/check-config.sh --seam measures what nginx does
  # with both -- they fall through to Rails, not to a 403 -- so what is left
  # to refuse is a directory serving nothing at all.

  test 'a locale directory above a page is allowed' do
    with_bundle do |dist|
      assert File.directory?("#{dist}/site/ru"), 'the fixture should have built a locale tree'
      assert_not File.exist?("#{dist}/site/ru/index.html"),
                 'the Russian home page is #160, not this'

      out, status = check("#{dist}/site")
      assert status.success?, "check-bundle.sh refused a locale directory:\n#{out}"
    end
  end

  test 'the asset directory is allowed' do
    with_bundle do |dist|
      assert File.directory?("#{dist}/site/_astro")
      assert_not File.exist?("#{dist}/site/_astro/index.html")

      out, status = check("#{dist}/site")
      assert status.success?, "check-bundle.sh refused the asset directory:\n#{out}"
    end
  end

  test 'every locale tree is stamped, not just the English one' do
    # build.sh substitutes @@REVISION@@ across every .html in the tree. When
    # that was one file the distinction did not exist; with three it does, and
    # a page still holding @@REVISION@@ would be shipped by a check that only
    # ever looked at _smoke/index.html.
    with_bundle do |dist|
      %w[_smoke ru/_smoke zh/_smoke].each do |page|
        html = File.read("#{dist}/site/#{page}/index.html")
        assert_match(/[0-9a-f]{40}/, html, "#{page} does not name the commit it was built from")
        assert_no_match(/@@/, html, "#{page} still has an unsubstituted token")
      end
    end
  end

  test 'the checks work on a relative path' do
    with_bundle do |dist|
      out, status = Open3.capture2e('bash', CHECK.to_s, 'dist/site', chdir: File.dirname(dist))

      assert status.success?, "check-bundle.sh refused a good bundle given a relative path:\n#{out}"
    end
  end

  # Each of these is a way the bundle does damage rather than nothing, and each
  # is checked by planting it rather than by reading the script.
  {
    'a path Rails owns' => lambda { |site|
      FileUtils.mkdir_p("#{site}/admin")
      File.write("#{site}/admin/index.html", 'x')
    },
    'the same path behind a locale prefix' => lambda { |site|
      FileUtils.mkdir_p("#{site}/ru/snapshots")
      File.write("#{site}/ru/snapshots/index.html", 'x')
    },
    'a directory with nothing under it' => lambda { |site|
      FileUtils.mkdir_p("#{site}/guide/empty")
    },
    'a symlink out of the tree' => ->(site) { File.symlink('/etc/passwd', "#{site}/leak") },
    'a file added after the manifest was written' => ->(site) { File.write("#{site}/stray.html", 'x') }
  }.each do |description, plant|
    test "the checks refuse #{description}" do
      with_bundle do |dist|
        plant.call("#{dist}/site")
        out, status = check("#{dist}/site")

        assert_not status.success?, "check-bundle.sh accepted #{description}:\n#{out}"
      end
    end
  end

  # Three sources that know nothing about each other. A list maintained by hand
  # is a list that rots -- which is exactly the shape of the bug #254 had just
  # fixed in robots.txt, where the rule named one of four spellings.
  test 'every route that cannot be a file is reserved' do
    unreserved = Rails.application.routes.routes.filter_map do |route|
      verb = route.verb.to_s
      next if verb.empty? || verb == 'GET'

      path = route.path.spec.to_s.delete_suffix('(.:format)')
      next if path.include?(':') || path.include?('*')

      path unless reserved?(path)
    end

    assert_empty unreserved.uniq, <<~MESSAGE.chomp
      These routes answer something other than GET and are not in
      deploy/static/reserved-paths:

        #{unreserved.uniq.join("\n        ")}

      A file in the bundle would answer the GET at the same address and the
      route would never be reached. nginx's static handler serves POST too.
    MESSAGE
  end

  test 'everything Rails serves out of public/ is reserved' do
    unreserved = Rails.root.join('public').children.filter_map do |entry|
      path = "/#{entry.basename}"
      path unless reserved?(entry.directory? ? "#{path}/x" : path)
    end

    assert_empty unreserved, <<~MESSAGE.chomp
      These exist in public/ and are not in deploy/static/reserved-paths:

        #{unreserved.join("\n        ")}

      RAILS_SERVE_STATIC_FILES=1, so Rails serves all of public/ and the router
      knows about none of it. A bundle file at the same address shadows it with
      nothing to say so.
    MESSAGE
  end

  test 'every path nginx answers without Rails is reserved' do
    vhost = Rails.root.join('deploy/nginx/sites-available/org.openipc').read
    # A location that aliases a directory, returns a status, or is internal is
    # answered by nginx itself -- so a bundle file at that address is either
    # shadowed by it or shadows it, and neither is a thing to discover later.
    from_disk = vhost.scan(%r{^    location (?:\^~ |= )?(/\S*) \{\n((?:.*\n)*?)    \}})
                     .select { |_path, body| body.match?(/^\s+(alias|root|return|internal)\b/) }
                     # Locations rooted in the static bundle ARE the seam, not
                     # competitors with it: `location /` carries the pages and
                     # `location ^~ /_astro/` the hashed assets (#159). A
                     # bundle file at those addresses is the point. Reserving
                     # them would make check-bundle.sh refuse the bundle's own
                     # contents. What guards the bare `/` is the root-index
                     # rule in check-bundle.sh instead.
                     .reject { |_path, body| body.match?(%r{^\s+root\s+/srv/www/static/}) }
                     .map(&:first)
                     # The catch-all itself. `location /` appears twice: on 443
                     # carrying the seam, caught by the root test above, and on
                     # port 80 as a redirect to https, which is not.
                     .reject { |path| path == '/' }

    refute_empty from_disk, 'this test is reading nothing out of the vhost'
    unreserved = from_disk.reject { |path| reserved?(path.end_with?('/') ? "#{path}x" : path) }

    assert_empty unreserved, <<~MESSAGE.chomp
      nginx answers these from disk or with a status of its own, and they are
      not in deploy/static/reserved-paths:

        #{unreserved.join("\n        ")}
    MESSAGE
  end

  # The other direction: an entry that matches nothing any more is folklore,
  # and folklore is how a list stops being read.
  test 'no reserved entry has stopped meaning anything' do
    known = Rails.application.routes.routes.map { |r| r.path.spec.to_s } +
            Rails.root.join('public').children.map { |c| "/#{c.basename}" } +
            Rails.root.glob('deploy/nginx/sites-available/*').flat_map { |f| f.read.lines.grep(/^\s*location /) }

    dead = RESERVED.reject do |rule|
      needle = rule.delete_prefix('*').chomp('/')
      needle.empty? || known.any? { |k| k.include?(needle) }
    end

    assert_empty dead, <<~MESSAGE.chomp
      These entries in deploy/static/reserved-paths match no route, no file in
      public/ and no nginx location:

        #{dead.join("\n        ")}

      Either the thing they protected is gone and the line should go with it,
      or it moved and the line no longer covers it.
    MESSAGE
  end

  # The scripts are the whole mechanism and nothing else executes them here.
  test 'the scripts parse' do
    ['static.sh', 'static/build.sh', 'static/check-bundle.sh', 'nginx/check-config.sh'].each do |script|
      path = Rails.root.join('deploy', script)
      assert path.executable?, "deploy/#{script} is not executable"
      _out, status = Open3.capture2e('bash', '-n', path.to_s)
      assert status.success?, "deploy/#{script} does not parse"
    end
  end

  # Not a new job. Master requires exactly the contexts `build` and `test`, so
  # a bundle that would shadow /admin can only block a merge from inside one of
  # them.
  test 'the bundle is published from a job that gates the merge' do
    # Psych reads the `on:` key as the boolean true; only `jobs` is read here,
    # so it does not matter -- but it is worth knowing before chasing it.
    workflow = YAML.safe_load(Rails.root.join('.github/workflows/build.yml').read)
    steps = workflow.fetch('jobs').fetch('build').fetch('steps')

    assert_equal %w[test build], workflow.fetch('jobs').keys,
                 'a third job would report without gating the merge'
    assert steps.any? { |s| s.dig('with', 'file').to_s.include?('deploy/static/Dockerfile') },
           'nothing in the build job publishes the static bundle'
    assert steps.any? { |s| s['run'].to_s.include?('check-bundle.sh') },
           'the build job publishes a bundle it never checked'
  end

  private

  # The same matching check-bundle.sh does: prefix, glob, or exact, after a
  # locale prefix is stripped.
  def reserved?(path)
    stripped = path.sub(%r{\A/(#{Multilang::IN_PATH.source})(/|\z)}, '/')

    RESERVED.any? do |rule|
      [path, stripped].any? do |p|
        if rule.end_with?('/') then "#{p}/".start_with?(rule)
        elsif rule.include?('*') then File.fnmatch(rule, p, File::FNM_PATHNAME)
        else p == rule
        end
      end
    end
  end
end
