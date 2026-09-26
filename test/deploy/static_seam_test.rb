# frozen_string_literal: true

require 'test_helper'

# The seam between the static bundle and Rails (#157).
#
# nginx's catch-all serves a file from the bundle when one is there and falls
# through to a named `@rails` location when it is not, so "extracted" and "has
# an index.html in the bundle" are the same statement. That is a lot of
# behaviour resting on four directives, and three of the ways to get it wrong
# are silent: the site keeps working and something else stops.
#
# What these hold is measured, not reasoned. The numbers quoted in the vhost
# comments came from nginx 1.26.3 in a container; deploy/nginx/check-config.sh
# re-runs that measurement on demand.
class StaticSeamTest < ActiveSupport::TestCase
  VHOSTS = {
    'org.openipc' => 'http://127.0.0.1:3000',
    'org.openipc.dev' => 'http://127.0.0.1:3001'
  }.freeze

  def vhost(name)
    Rails.root.join("deploy/nginx/sites-available/#{name}").read
  end

  # The block body, comments stripped.
  #
  # Two things this has to get right. There are two `location /` blocks in each
  # vhost -- the port-80 one only redirects to https, and it comes first in the
  # file, which is what the earliest version of this helper picked up. And the
  # vhost explains at length why it does NOT do several things, so a naive
  # include? matches the prose and reports the opposite of the truth.
  def block(name, header, containing:)
    blocks = vhost(name).scan(/^\s*#{Regexp.escape(header)} \{\n(.*?)\n\s*\}\n/m).flatten
    matching = blocks.select { |b| b.include?(containing) }

    assert_equal 1, matching.length,
                 "#{name}: expected exactly one `#{header}` containing `#{containing}`, " \
                 "found #{matching.length} among #{blocks.length} blocks"
    matching.first.lines.reject { |l| l.strip.start_with?('#') }.join
  end

  # The catch-all that carries the seam, never the port-80 redirect.
  def seam(name) = block(name, 'location /', containing: 'try_files')

  def fallback(name) = block(name, 'location @rails', containing: 'proxy_pass')

  test 'the catch-all tries the bundle and then falls through to Rails' do
    VHOSTS.each_key do |name|
      directives = seam(name)

      assert_includes directives, 'root /srv/www/static/',
                      "#{name}: `location /` has no document root, so try_files resolves against nothing"
      try_files = directives[/try_files (.*);/, 1]
      assert try_files, "#{name}: `location /` does not try the bundle at all"
      assert_equal '@rails', try_files.split.last, <<~MESSAGE.chomp
        #{name}: try_files ends in `#{try_files.split.last}`, not `@rails`.

        The last element is what the seam answers when the bundle holds
        nothing, which today is every address on the site. Anything but a
        named location here means nginx answers instead of Rails -- a 404 or a
        403 where there is a working page.
      MESSAGE
    end
  end

  # The one that would take the front page down on the day it shipped.
  #
  # try_files decides file-test versus directory-test from the literal element
  # at parse time, so an element written with a trailing slash tests for a
  # directory. A directory that matches goes to the index module, which answers
  # 403 when there is no index.html in it -- and with an empty bundle the
  # directory that always exists is the bundle root, so `/` answers 403.
  # Measured on 1.26.3: `$uri $uri/index.html` answers rails 200, `$uri $uri/`
  # answers 403.
  test 'no try_files element tests for a directory' do
    VHOSTS.each_key do |name|
      elements = seam(name)[/try_files (.*);/, 1].split
      offending = elements.select { |e| e.end_with?('/') }

      assert_empty offending, <<~MESSAGE.chomp
        #{name}: try_files has an element ending in a slash: #{offending.join(' ')}

        Only such an element tests for a directory, and a directory with no
        index.html answers 403 rather than falling through. With an empty
        bundle the bundle root is such a directory, so this form answers 403
        for `/` -- the busiest URL on the site.
      MESSAGE
    end
  end

  test 'the fallback proxies to the application' do
    VHOSTS.each do |name, upstream|
      directives = fallback(name)

      assert_includes directives, "proxy_pass #{upstream};",
                      "#{name}: @rails does not proxy to #{upstream}"
    end
  end

  # nginx refuses `proxy_pass` with a URI part inside a named location, so this
  # is a configuration that will not start rather than one that misbehaves --
  # but it is a copy-paste away, the old block had one, and finding it here
  # beats finding it when push-nginx.sh reloads.
  test 'the fallback proxy_pass carries no URI part' do
    VHOSTS.each_key do |name|
      pass = fallback(name)[/proxy_pass (\S+);/, 1]

      assert_not pass.end_with?('/'), <<~MESSAGE.chomp
        #{name}: @rails proxies to `#{pass}`, which has a URI part.

        nginx refuses a URI part in a named location -- "proxy_pass cannot have
        URI part in location given by regular expression, or inside named
        location". nginx -t fails and the reload does not happen.
      MESSAGE
    end
  end

  # Phase order, measured. limit_conn runs in preaccess and try_files in
  # precontent, so the configuration that counts is the location the request
  # landed in first. With the cap at 1 and four concurrent slow transfers on
  # 1.26.3: in `location /`, 429 429 429 200; in `@rails`, 200 200 200 200.
  test 'admission control sits where the phase engine can see it' do
    prod_catch_all = seam('org.openipc')
    prod_rails = fallback('org.openipc')

    assert_includes prod_catch_all, 'limit_conn site_conc', <<~MESSAGE.chomp
      `location /` declares no limit_conn, so it inherits the http-level
      `limit_conn per_subnet 20` -- and because limit_conn is evaluated once
      per request, in the first location the request reaches, that inherited
      cap is the only one that runs. The site_conc pool that ended the
      2026-09-03 outage would be silently replaced by a per-address twenty.
    MESSAGE

    assert_not_includes prod_rails, 'limit_conn', <<~MESSAGE.chomp
      @rails declares a limit_conn. It cannot run: the request has already
      passed preaccess in `location /`, and limit_conn returns early on every
      pass after the first. Measured with the cap at 1 and four concurrent
      slow transfers, @rails alone sheds nothing at all.

      A directive that looks like it is doing the work and is not is worse
      than its absence, in a file that is read the way this one is.
    MESSAGE
  end

  # add_header at location level REPLACES every inherited one. The production
  # vhost has documented this since #233; the dev vhost has a second header
  # that nothing was previously at risk of dropping.
  test 'the new locations repeat every header they would otherwise drop' do
    {
      'org.openipc' => ['add_header Strict-Transport-Security'],
      'org.openipc.dev' => ['add_header Strict-Transport-Security', 'add_header X-Robots-Tag']
    }.each do |name, required|
      { 'location /' => seam(name), 'location @rails' => fallback(name) }.each do |header, directives|
        required.each do |directive|
          assert_includes directives, directive, <<~MESSAGE.chomp
            #{name}: `#{header}` uses add_header and does not repeat
            `#{directive}`.

            add_header at location level replaces all inherited add_header
            directives, so this location would serve every page it answers
            without it.
          MESSAGE
        end
      end
    end
  end

  # The witness. Without it, telling a static hit from a Rails render means
  # guessing from timing, and the epic names a stale bundle shadowing a fixed
  # Rails page as the most likely way this goes wrong.
  test 'each side of the seam says which one it is' do
    VHOSTS.each_key do |name|
      assert_includes seam(name), 'add_header X-Served-By static always',
                      "#{name}: a page served from the bundle does not say so"
      # Since #302 the fallback answers the router's redirects and 410s
      # itself, so the witness is a variable: `rails` for what reaches the
      # application, `nginx` for what the generated map answered.
      assert_includes fallback(name), 'add_header X-Served-By $openipc_route_by always',
                      "#{name}: a page rendered by Rails does not say so"
    end
    routes = Rails.root.join('deploy/nginx/conf.d/openipc-redirects.conf').read
    assert_match(/map \$openipc_route_action \$openipc_route_by \{\s*rails\s+rails;\s*default\s+nginx;/, routes,
                 'what reaches Rails must still say rails')
  end

  # A rollback rehearsal on dev must not be able to change what openipc.org
  # serves -- the same rule that gives dev its own storage, wall and database.
  # --- the installer -------------------------------------------------------
  #
  # deploy/static.sh is the other half of the seam: nginx decides which side
  # answers, this decides which bundle is there to answer from. Three of its
  # lines are the kind that look right and are not.

  INSTALLER = Rails.root.join('deploy/static.sh').read.freeze

  # Directives only. The script explains at length why it does NOT use ln -sfn
  # on `current`, and a naive include? matches that explanation.
  def installer
    INSTALLER.lines.reject { |l| l.strip.start_with?('#') }.join
  end

  # `ln -sfn` is unlink() then symlink(), so there is a window in which
  # `current` does not exist. That window is harmless here -- every request in
  # it falls through to Rails -- but `mv` is atomic and costs nothing.
  #
  # The -T is the part that is not optional. Without it, `mv tmp current` where
  # current is a symlink to a directory FOLLOWS the symlink and moves the new
  # link inside the old release: current still points at the old bundle and the
  # command reports success.
  test 'the symlink flip is atomic and cannot move the link inside the old release' do
    assert_match(/mv -Tf? "[^"]*" "[^"]*current"/, installer,
                 'the flip does not use `mv -T`, so it can silently leave the old bundle in place')
    assert_no_match(%r{ln -sfn [^\n]*/current"}, installer,
                    'the flip links `current` directly, so the path briefly does not exist')
  end

  # An extracted bundle is `bundle-<sha>/{site,MANIFEST,REVISION}` and nginx's
  # root is the `current` symlink, so linking it one level too high serves the
  # manifest at /MANIFEST and puts every page one directory too deep. Both ends
  # have to agree on which directory is the served tree, and they are written
  # in two different languages in two different files.
  test 'the directory nginx serves is the one the build produces' do
    build = Rails.root.join('deploy/static/build.sh').read

    assert_includes build, 'mkdir -p "$OUT/site"',
                    'build.sh does not put the served tree in site/'
    assert_match(%r{served_tree\(\) \{ printf 'bundle-%s/site'}, installer,
                 'static.sh links `current` somewhere other than the built site/ directory')
  end

  # The same rule deploy.sh applies to the application image, at the other end
  # of the same pipe.
  test 'a bundle that could not be rolled back to is refused' do
    assert_includes installer, '^[0-9a-f]{40}$',
                    'static.sh accepts an image whose revision is not a commit, which could never be rolled back to'
  end

  # deploy/static/README.md says the extracted directories are the rollback
  # store and the registry is only the delivery path -- "so it keeps working
  # whatever GHCR's untagged-version cleanup decides to do". The first version
  # of do_install pulled before it looked on disk, which made that sentence
  # false: a rollback during a registry outage, which is exactly when one is
  # wanted, would have failed with a bundle sitting there intact.
  #
  # Proven on dev by running `rollback` with `docker` stubbed out to fail on
  # any call; this keeps the ordering from drifting back.
  test 'a bundle already on disk is installed without touching the registry' do
    body = installer[/^do_install\(\) \{(.*?)^\}/m, 1]

    assert body, 'static.sh has no do_install'
    on_disk = body.index('intact "$root" "$ref"')
    pull = body.index('resolve_image')

    assert on_disk, 'do_install never checks whether the bundle is already here'
    assert pull, 'do_install never resolves a reference'
    assert on_disk < pull, <<~MESSAGE.chomp
      do_install reaches for the registry before it looks on disk.

      A rollback goes through this function, and the moment it needs a pull it
      stops working during exactly the outage that makes someone want it.
      Ten bundles are kept on disk to make a rollback a symlink flip; check
      for one before resolving anything.
    MESSAGE
  end

  # Pull one reference, address another, and it works only because `docker
  # create` quietly pulls a second time -- until the SHA tag is gone from the
  # registry while the branch tag is still there.
  test 'the resolved image is tagged locally before anything extracts it' do
    assert_match(/docker tag "\$image" "\$\{REGISTRY_IMAGE\}:\$\{revision\}"/, installer,
                 'resolve_image pulls a tag and hands extraction a different one')
  end

  # `docker create --rm` sets AutoRemove, which only fires on stop -- and this
  # container is never started, so every failed install would leak one.
  test 'the extraction container is removed however the install ends' do
    assert_match(/trap cleanup EXIT/, installer)
    assert_includes installer, 'docker rm -f',
                    'a created container is never removed, so failed installs pile up in docker ps -a'
    assert_no_match(/docker create --rm/, installer)
  end

  # Every path the installer asserts must still reach Rails has to be a real
  # address. A typo here is a check that passes because nothing answers it.
  test 'the paths the installer guards are real addresses' do
    paths = installer[/MUST_NOT_BE_STATIC=\((.*?)\)/m, 1].split

    refute_empty paths, 'the installer verifies nothing after a flip'
    paths.each do |path|
      # A route, or a file Rails serves straight out of public/ -- robots.txt
      # is the second kind, and the router knows nothing about it.
      next if Rails.root.join('public', path.delete_prefix('/')).file?

      recognized = begin
        Rails.application.routes.recognize_path(path)
      rescue ActionController::RoutingError
        { action: 'route_not_found' }
      end

      assert_not_equal 'route_not_found', recognized[:action],
                       "static.sh guards #{path}, which is neither a route nor a file in public/ -- " \
                       'it would pass whatever the bundle did'
    end
  end

  test 'dev and production serve different bundles' do
    roots = VHOSTS.keys.map { |name| seam(name)[/root (\S+);/, 1] }

    assert_equal roots.uniq.length, roots.length,
                 "dev and production share the document root #{roots.first}; " \
                 'installing a bundle on dev would change production'
  end
end
