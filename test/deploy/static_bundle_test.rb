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

  # Every fixed address the router knows, redirects included.
  #
  # `(/:locale)` is the optional prefix #154 put on every public route, and it
  # is exactly the part the bundle expresses as a directory rather than a
  # parameter -- /ru/donate is a file. Stripped before comparing, or every
  # marketing route would look like a dynamic one and match nothing.
  def routed_paths
    @routed_paths ||= Rails.application.routes.routes.filter_map do |route|
      path = route.path.spec.to_s.delete_suffix('(.:format)')
      # Two spellings, because the root is declared inside the scope rather
      # than under it: `get '/donate'` becomes "(/:locale)/donate" and `root`
      # becomes "/(:locale)". Both mean the same optional prefix.
      path = path.sub(%r{\A\(/:locale\)}, '').sub(%r{\A/\(:locale\)}, '')
      next if path.include?(':') || path.include?('*')

      path.empty? ? '/' : path
    end.to_set.freeze
  end

  # --- what the bundle claims, against what Rails routes (#160) --------------
  #
  # frontend/apps/site/src/lib/page-paths.ts is the list of addresses the
  # bundle serves. It is TypeScript, so nothing in Ruby validates it, and it is
  # the one file in the repository that can silently shadow a Rails route: the
  # seam serves a file before it asks Rails, without a line in the log.
  #
  # Read as text rather than executed. A Node process inside `bin/rails test`
  # would put the frontend toolchain on the critical path of a Ruby suite, and
  # the list is a literal array of string fields -- if it ever stops being one,
  # this stops finding paths and the count assertion below fails loudly rather
  # than passing vacuously.
  PAGE_PATHS_TS = Rails.root.join('frontend/apps/site/src/lib/page-paths.ts')

  def bundle_paths
    @bundle_paths ||= PAGE_PATHS_TS.read.scan(/^\s*\{ path: '([^']+)'/).flatten.freeze
  end

  test 'the bundle claims a plausible number of addresses' do
    # Guards the scan above: a refactor that changes the file's shape must not
    # quietly turn every assertion below into a test of the empty set.
    assert_operator bundle_paths.size, :>=, 20,
                    "only found #{bundle_paths.size} paths in #{PAGE_PATHS_TS.basename}; has its shape changed?"
  end

  test 'every address the bundle claims is a real Rails route' do
    known = routed_paths

    # /_smoke is the diagnostic. It is the one address in the bundle with no
    # Rails route behind it, deliberately: nothing should answer it but the
    # bundle, and that is what proves the seam is alive.
    unrouted = bundle_paths.reject { |path| path == '/_smoke' || known.include?(path) }

    assert_empty unrouted, <<~MESSAGE.chomp
      These addresses are in the static bundle but config/routes.rb has no
      route for them:

        #{unrouted.join("\n        ")}

      Either the path is a typo -- in which case the bundle serves a page at an
      address nothing links to -- or the Rails route was deleted before the
      bundle stopped claiming it, which leaves no fallback when the bundle is
      rolled back.
    MESSAGE
  end

  test 'every Rails address the bundle links to is a real route' do
    # The other half of the link check in
    # frontend/apps/site/src/lib/pages.build.test.ts. That one asserts every
    # internal href in the built tree is either a bundle page or one of these;
    # this one asserts these exist. A link to a path the router does not know
    # falls through the catch-all to a 302 home, which looks like a working
    # link right up until somebody clicks it.
    ts = Rails.root.join('frontend/apps/site/src/lib/rails-paths.ts').read
    listed = ts[/RAILS_PATHS[^=]*=\s*\[(.*?)\]/m, 1].to_s.scan(/'([^']+)'/).flatten

    assert_operator listed.size, :>=, 3, 'found no paths in rails-paths.ts; has its shape changed?'

    # Matched against the route table rather than through recognize_path.
    # `match "*unmatched"` matches everything, so recognize_path never raises
    # and answers application#route_not_found for a path that does not exist --
    # and it does the same for a route defined with `redirect`, which
    # /supported-hardware is. A redirect is a fine destination; the catch-all
    # is not, and only the table tells them apart.
    unrouted = listed.reject { |path| routed_paths.include?(path) }

    assert_empty unrouted, <<~MESSAGE.chomp
      The static pages link these, and config/routes.rb does not route them:

        #{unrouted.join("\n        ")}
    MESSAGE
  end

  test 'the wizard the catalogue links to is a route Rails still has' do
    # rails-paths.ts carries a pattern as well as a list (#162): every row of
    # the catalogue links one address per SoC --
    # /cameras/vendors/<vendor>/socs/<soc> -- which is Rails' until #163. A
    # list of 126 strings would be a second copy of the catalogue, so the
    # frontend matches a shape and this asserts the shape is real.
    #
    # Not by re-running the TypeScript regex in Ruby: a regex parsed out of one
    # language and executed in another tests the parser. What matters here is
    # that the address those links have exists, and that the frontend has not
    # quietly dropped the pattern that lets them through.
    ts = Rails.root.join('frontend/apps/site/src/lib/rails-paths.ts').read

    assert_match(/RAILS_PATTERNS/, ts, 'rails-paths.ts no longer carries the wizard pattern')
    assert_match(%r{cameras\\?/vendors}, ts, "the pattern no longer names the wizard's tree")

    # Asked of the router itself: `routed_paths` holds the static addresses,
    # and this one carries two parameters.
    helper = Rails.application.routes.url_helpers
    assert_equal '/cameras/vendors/probe/socs/ps1000',
                 helper.cameras_vendor_soc_path(vendor_id: 'probe', id: 'ps1000'),
                 'the catalogue links at an address config/routes.rb does not route'
  end

  test 'the baked support goal is the one config/support_goal.yml sets' do
    # The donate page and the home band print "help us reach N", and N is a
    # setting somebody raises by pull request (#198) -- so the prerendered copy
    # of it cannot drift. The build has no Ruby, which is why there is a copy
    # at all; this is what makes raising the goal one edit and a red test.
    ts = Rails.root.join('frontend/apps/site/src/data/support-goal.ts').read
    baked = ts[/SUPPORT_GOAL\s*=\s*(\d+)/, 1]&.to_i

    assert baked, 'found no SUPPORT_GOAL in support-goal.ts; has its shape changed?'
    assert_equal SupportStats.goal, baked, <<~MESSAGE.chomp
      config/support_goal.yml says #{SupportStats.goal} and the static pages say #{baked}.

      Both halves of the site quote this number, and they must quote the same one.
    MESSAGE
  end

  test 'no address the bundle claims is reserved' do
    # The two lists are written for opposite purposes and must not overlap:
    # reserved-paths is what the bundle must never contain, and page-paths.ts
    # is what it does contain. check-bundle.sh refuses the overlap at install
    # time; this says so at the point somebody adds the second entry.
    collisions = bundle_paths.select { |path| reserved?(path) }

    assert_empty collisions, <<~MESSAGE.chomp
      These addresses are both claimed by the bundle and reserved against it:

        #{collisions.join("\n        ")}

      deploy/static/check-bundle.sh would refuse the bundle at install time.
    MESSAGE
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
