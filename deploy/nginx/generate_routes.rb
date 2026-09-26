# frozen_string_literal: true

# Writes deploy/nginx/conf.d/openipc-redirects.conf from config/routes.rb (#302).
#
#   bin/rails runner deploy/nginx/generate_routes.rb            # write the file
#   bin/rails runner deploy/nginx/generate_routes.rb --check    # exit 1 if it is stale
#
# nginx answers what Rails' router answered without Rails: every redirect, every
# retired (410) address, and the catch-all's 302 to the home page. The map is
# GENERATED, never hand-copied, because the router is the specification and a
# copy starts disagreeing with it the day after it is made.
# test/deploy/nginx_routes_test.rb holds the committed file to this output.
#
# How each route becomes a line:
#
#   - Its path pattern is Journey's own regexp, rewritten for PCRE, and matched
#     against "$request_method $uri" so a GET route does not answer a POST --
#     exactly as the router, where a POST to /home fell through to the
#     catch-all rather than redirecting.
#   - A redirect's target is found by CALLING it, not by reading it: a sample
#     request with a query string goes through the route, and what comes back
#     says where it points, whether the query travels (keep_query), and whether
#     the locale prefix does (/ru/supported-hardware). Blocks cannot be read;
#     they can be run.
#   - Routes that render something stay Rails' (and later Go's). Everything
#     after `*unmatched` is unreachable -- the catch-all is matched first --
#     and is left out.
#
# The catch-all is the map's default, so a path no route claims is answered by
# nginx: 302 to "/", or to "/ru" / "/zh" under a locale prefix. It sits in
# `location @rails`, after try_files, so a file in the static bundle still wins
# over all of this, as it did.
#
# One known difference, and it is nginx's: $uri is decoded, so an encoded slash
# counts as a slash. /admin%2F.env was the catch-all's 302 in Rails and is
# /admin/*'s 410 here -- a refusal either way, for a scanner probe. Replaying a
# day of production traffic (2026-09-25) through this map, it was the only one
# of 2,237 addresses nginx now answers whose status or Location changed.
require 'rack/mock'

# The router, read as nginx map entries.
module NginxRoutes
  OUT = Rails.root.join('deploy/nginx/conf.d/openipc-redirects.conf')
  HOST = 'openipc.org'
  ORIGIN = %r{\Ahttps?://#{Regexp.escape(HOST)}(?::\d+)?}

  # Development-only engines the router carries outside production.
  SKIP = %r{\A/rails/(info|mailers)}

  # The one redirect whose target is not a constant: it names a fingerprinted
  # asset, which dies with Rails (#304). The image is served as a file from
  # /images/ instead (deploy/legacy-images), so no route is needed.
  NOT_A_REDIRECT_ANY_MORE = ['/images/logo_openipc.png(.:format)'].freeze

  Entry = Struct.new(:key, :action, :target, :source)

  module_function

  def routes
    list = Rails.application.routes.routes.to_a
    stop = list.index { |r| spec(r).start_with?('/*unmatched') } or raise 'no catch-all route'
    list.first(stop).reject { |r| SKIP.match?(spec(r)) || NOT_A_REDIRECT_ANY_MORE.include?(spec(r)) }
  end

  def spec(route) = route.path.spec.to_s

  # The route's own endpoint, under the constraint wrappers the router adds.
  def endpoint(route)
    app = route.app
    app = app.app while app.is_a?(ActionDispatch::Routing::Mapper::Constraints)
    app
  end

  def methods(route)
    case route.verb
    when '' then '[A-Z]+'
    when 'GET' then '(?:GET|HEAD)'
    else "(?:#{route.verb})"
    end
  end

  # Journey's regexp as PCRE, anchored after the method. Groups stay capturing
  # so a locale prefix, when a variant requires it, is $1. The router matches
  # after Journey's normalize_path, which drops trailing slashes -- /home/ is
  # /home, /ru/ is the Russian home page -- and nginx has already merged
  # repeated slashes into $uri.
  def pcre(route, locale: nil)
    source = route.path.to_regexp.source
    source = source.sub('(?:/((?-mix:ru|zh)))?', locale == :required ? '/(ru|zh)' : '') if locale
    source = source.sub('\\A', '').sub('\\Z', '/*$').gsub('(?-mix:', '(?:').gsub('(?m-ix:', '(?s:')
    "~^#{methods(route)} #{source}"
  end

  # One or two entries per route: a locale-scoped route is split into the form
  # with the prefix and the form without, so each can carry its own target.
  def variants(route)
    return [[nil, sample(route, nil)]] unless spec(route).start_with?('(/:locale)')

    [[:required, sample(route, 'ru')], [:absent, sample(route, nil)]]
  end

  def sample(route, locale)
    path = spec(route).sub('(/:locale)', locale ? "/#{locale}" : '')
                      .gsub('(.:format)', '').gsub(%r{\(/\*\w+\)}, '/sample/deep')
                      .gsub(/\*\w+/, 'sample/deep').gsub(/:\w+/, 'x1').gsub(/[()]/, '')
    path.empty? ? '/' : path
  end

  def call(path, query = nil)
    env = Rack::MockRequest.env_for("https://#{HOST}#{path}#{"?#{query}" if query}",
                                    'HTTP_X_FORWARDED_PROTO' => 'https', 'HTTP_HOST' => HOST)
    status, headers, = Rails.application.routes.call(env)
    [status.to_s, (headers['location'] || headers['Location']).to_s.sub(ORIGIN, '')]
  end

  # Where a redirect route sends a visitor, as an nginx value. Internal targets
  # are written as paths (nginx makes them absolute from the request, as Rails
  # did); a travelling query string becomes $is_args$args; a travelling locale
  # prefix becomes $1.
  def redirect(route, locale, path)
    status, target = call(path)
    target = '/' if target.empty?
    target += '$is_args$args' if call(path, 'q=probe').last.include?('q=probe')
    target = localized(route, target) if locale == :required
    [status, target]
  end

  def localized(route, target)
    return target unless target.match?(%r{\A/ru(/|\z|\$)})

    zh = call(sample(route, 'zh')).last
    raise "#{spec(route)}: the locale does not travel as a prefix (#{zh})" unless zh.start_with?('/zh')

    target.sub(%r{\A/ru}, '/$1')
  end

  def gone?(route)
    app = endpoint(route)
    app.is_a?(Proc) && app.call({}).first == 410
  end

  def entry(route, locale, path)
    key = pcre(route, locale: locale)
    source = "#{route.verb.presence || 'ANY'} #{spec(route)}"
    redirecting = endpoint(route).is_a?(ActionDispatch::Routing::Redirect)
    return Entry.new(key, *redirect(route, locale, path), source) if redirecting
    return Entry.new(key, '410', '', source) if gone?(route)

    Entry.new(key, 'rails', '', source)
  end

  def entries
    routes.flat_map { |route| variants(route).map { |locale, path| entry(route, locale, path) } }
  end

  # What Rails answers before the router: every file under public/ (served by
  # the static middleware) and the asset pipeline. They are Rails', not the
  # catch-all's.
  def public_entries
    names = Rails.public_path.children.map { |p| p.basename.to_s }.sort - %w[files dl] # nginx serves those
    names.map do |name|
      tail = Rails.public_path.join(name).directory? ? '/' : '$'
      Entry.new("~^[A-Z]+ /#{Regexp.escape(name)}#{tail}", 'rails', '', "public/#{name}")
    end + [Entry.new('~^[A-Z]+ /assets/', 'rails', '', 'the asset pipeline')]
  end
end

# The entries, written as nginx configuration.
module NginxRoutesConf
  HEADER = <<~CONF
    # GENERATED by deploy/nginx/generate_routes.rb from config/routes.rb. Do not edit:
    #   bin/rails runner deploy/nginx/generate_routes.rb
    # test/deploy/nginx_routes_test.rb fails when this and the router disagree.
    #
    # What Rails' router answered without rendering anything (#302), for
    # `location @rails` to answer itself. Keyed on "$request_method $uri",
    # first match wins, in the router's own order.
    #
    #   rails     the request goes on to the application
    #   301 302   a redirect to $openipc_route_target
    #   410       retired
    #   catchall  nothing claims it: 302 to the home page in its language
  CONF

  FOOTER = <<~CONF
    map $openipc_route_action $openipc_route_by {
        rails   rails;
        default nginx;
    }

    # Rails sent `Cache-Control: no-cache` on every one of these answers. What
    # reaches Rails keeps whatever Rails says (an empty value adds nothing).
    map $openipc_route_action $openipc_route_cache {
        rails   "";
        default no-cache;
    }
  CONF

  module_function

  def quote(value)
    %("#{value.gsub('\\', '\\\\\\\\').gsub('"', '\"')}")
  end

  def lines(all, width, field)
    all.map { |e| "    #{quote(e.key).ljust(width)} #{quote(e[field])};  # #{e.source}" }.join("\n")
  end

  def render
    all = NginxRoutes.public_entries + NginxRoutes.entries
    width = all.map { |e| e.key.length }.max + 2
    [HEADER,
     "map \"$request_method $uri\" $openipc_route_action {\n#{lines(all, width, :action)}\n    default catchall;\n}\n",
     "map \"$request_method $uri\" $openipc_route_target {\n#{lines(all, width, :target)}\n" \
     "    \"~^[A-Z]+ /(ru|zh)(?:/|$)\"  \"/$1\";  # the catch-all, under a locale prefix\n    default \"/\";\n}\n",
     FOOTER].join("\n")
  end
end

# Loaded by test/deploy/nginx_routes_test.rb for the modules alone.
return if ENV['NGINX_ROUTES_LIBRARY'] == '1'

text = NginxRoutesConf.render
if ARGV.include?('--check')
  current = NginxRoutes::OUT.exist? ? NginxRoutes::OUT.read : ''
  abort "#{NginxRoutes::OUT} is stale: bin/rails runner deploy/nginx/generate_routes.rb" unless current == text
  puts 'up to date'
else
  File.write(NginxRoutes::OUT, text)
  puts "wrote #{NginxRoutes::OUT}"
end
