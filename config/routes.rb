Rails.application.routes.draw do
  # Ships BEFORE the ?locale= redirects, deliberately (#154, and #142 says so in
  # as many words): if the sitemap and the redirects land together, a ranking
  # movement afterwards cannot be attributed to either. This one tells search
  # engines the three-language structure exists; the redirects come once that
  # has been crawled.
  get '/sitemap.xml', to: 'sitemaps#show', defaults: { format: 'xml' }

  # Healthcheck for the container runtime and the deploy script. Must stay above
  # the "*unmatched" catch-all below, which redirects instead of 404ing.
  get "/up", to: proc { [200, { "Content-Type" => "text/plain" }, ["ok"]] }

  # The locale lives in the path, not in a query parameter or a cookie (#154).
  #
  # English stays at `/`: every indexed URL, forum link and wiki link on the
  # site is English, and moving them would be a cost with no benefit. Russian
  # and Chinese gain a prefix, so `/ru/donate` and `/zh/get-started` are their
  # own addresses and a shared cache can finally key on the URL -- which is the
  # thing the whole epic is waiting for.
  #
  # The constraint is what keeps `(:locale)` from swallowing the rest of the
  # site: without it `/donate` would parse as locale "donate".
  #
  # Only pages a visitor reads are in here. The API, the admin area, the
  # healthcheck, the external redirects and the firmware download are not
  # translated and gain nothing from a prefix.
  # MARKED FOR DELETION, not before 2026-10-22 (#160).
  #
  # Every `pages#` route below except `root` is now served from the static
  # bundle. They stay because they are the fallback: nginx's try_files walks
  # past a missing file and ends at @rails, so a bundle that is rolled back,
  # half-installed or absent means these answer -- which is the property that
  # makes the cutover reversible by one symlink flip.
  #
  # Thirty days is how long that matters. After it, a rollback would be to a
  # bundle that is itself newer than these views, and keeping two renderings of
  # the same page is how the two start disagreeing. Delete the actions, the
  # views, app/helpers/pages_helper.rb's constants and
  # test/controllers/relaunch_pages_test.rb together; the assertions live on in
  # frontend/apps/site/src/lib/pages.build.test.ts.
  #
  # `root` is NOT in that list. `/` stays on Rails permanently: it renders per
  # Accept-Language and declares `Vary: Accept-Language`, which a file cannot
  # do. See the comment on rule 3 in deploy/static/check-bundle.sh.
  scope '(:locale)', locale: Multilang::IN_PATH do
    root 'pages#home'

    get '/get-started', to: 'pages#get_started'
    get '/low-latency', to: 'pages#low_latency'
    get '/teleoperation', to: 'pages#teleoperation'
    get '/edge-ai', to: 'pages#edge_ai'
    get '/video-encoding', to: 'pages#video_encoding'
    get '/isp-sensors', to: 'pages#isp_sensors'
    get '/reverse-engineering', to: 'pages#reverse_engineering'
    get '/turnkey-hardware', to: 'pages#turnkey_hardware'
    get '/digital-twins', to: 'pages#digital_twins'
    get '/ecosystem',   to: 'pages#ecosystem'
    get '/business',    to: 'pages#business'
    get '/community',   to: 'pages#community'
    get '/donate',      to: 'pages#donate'
    get '/privacy',     to: 'pages#privacy'
  end

  # The pre-relaunch structure, redirected rather than dropped. These URLs are
  # in search results, in forum posts and in the wiki, and none of that is ours
  # to edit. 301 so the ones that are indexed transfer rather than compete.
  #
  # /home was the temporary URL the homepage answered on while it was being
  # built and nothing linked to it; it is the root now.
  #
  # redirect('/path') drops the query string, and locale lives in it: a link to
  # /introduction?locale=ru landed on the homepage in whatever language the
  # browser asked for. keep_query preserves it, so a localized legacy link
  # stays in its language across the move.
  keep_query = lambda do |to|
    redirect { |_params, request| request.query_string.present? ? "#{to}?#{request.query_string}" : to }
  end

  get '/home',                to: keep_query.call('/')
  get '/introduction',        to: keep_query.call('/')
  get '/aaa',                 to: keep_query.call('/')
  get '/fpv',                 to: keep_query.call('/low-latency')
  get '/our-projects',        to: keep_query.call('/ecosystem')
  get '/our-software',        to: keep_query.call('/ecosystem')
  get '/our-channels',        to: keep_query.call('/community')
  get '/support-open-source', to: keep_query.call('/donate')
  # 302, not 301: /about is meant to become a page of its own, and a 301 is
  # cached by browsers indefinitely -- it would outlive the decision.
  get '/about', to: redirect(status: 302) { |_params, request|
    request.query_string.present? ? "/community?#{request.query_string}" : '/community'
  }
  scope('(:locale)', locale: Multilang::IN_PATH) { get '/majestic-endpoints', to: 'pages#majestic_endpoints' }

  get '/coupler',     to: redirect('https://github.com/OpenIPC/coupler/')
  get '/firmware',    to: redirect('https://github.com/OpenIPC/firmware/')
  get '/ipctool',     to: redirect('https://github.com/OpenIPC/ipctool/')
  get '/microbe-web', to: redirect('https://github.com/OpenIPC/microbe-web/')
  get '/smolrtsp',    to: redirect('https://github.com/OpenIPC/smolrtsp/')
  get '/yaml-cli',    to: redirect('https://github.com/OpenIPC/yaml-cli/')
  get '/wiki',        to: redirect('https://github.com/OpenIPC/wiki/')

  get '/hardware',    to: redirect('/supported-hardware/featured')
  get '/ru/installation.md', to: redirect('https://github.com/OpenIPC/wiki/blob/master/ru/installation.md')
  # A legacy URL people still embed elsewhere -- five hits a fortnight, but
  # they are somebody else's pages and should not break. It used to redirect to
  # cdn.themactep.com, which is a maintainer's personal domain rather than
  # anything this project runs; the image is now ours and served from our own
  # assets. Resolved per request so it follows the fingerprint.
  get '/images/logo_openipc.png',
      to: redirect { |_params, _request| ActionController::Base.helpers.asset_path('logo_openipc.png') }
  get '/devices/hs303/', to: redirect('https://github.com/OpenIPC/wiki/blob/master/ru/hardware-hs303.md')
  get '/install_switcam_hs303', to: redirect('https://github.com/OpenIPC/wiki/blob/master/ru/hardware-hs303.md')

  # FIXME: combine with above
  get '/coupler(/*any)',     to: redirect('https://github.com/OpenIPC/coupler')
  get '/firmware(/*any)',    to: redirect('https://github.com/OpenIPC/firmware')
  get '/ipctool(/*any)',     to: redirect('https://github.com/OpenIPC/ipctool')
  get '/microbe-web(/*any)', to: redirect('https://github.com/OpenIPC/microbe-web')
  get '/smolrtsp(/*any)',    to: redirect('https://github.com/OpenIPC/smolrtsp')
  get '/yaml-cli(/*any)',    to: redirect('https://github.com/OpenIPC/yaml-cli')
  get '/wiki(/*any)',        to: redirect('https://github.com/OpenIPC/wiki')

  get '/SDK', to: redirect('/supported-hardware')
  get '/sponsor', to: redirect('/donate')

  scope '(:locale)', locale: Multilang::IN_PATH do
    get '/green_life', to:'pages#green_life'
    # Not linked from anywhere while there is nothing to sell -- see the footer.
    # The route and the page stay: the shop is expected back, plausibly through
    # Open Collective, and deleting them would mean writing it all again.
    get '/merchandise', to: 'pages#merchandise'
    get '/our-team', to: 'pages#our_team'
    get '/stages-of-firmware-development', to: 'pages#stages_of_firmware_development'
    get '/utilities', to: 'pages#utilities'
    get '/web-interface', to: 'pages#web_interface'
  end

  # Localized, because the navbar links here from every page. Unscoped, the
  # "Hardware" item on a Russian page pointed at /ru/supported-hardware, which
  # was not a route and dropped the visitor on the ENGLISH homepage. A redirect
  # target has to carry the prefix forward or it is a language change disguised
  # as a navigation click.
  scope '(:locale)', locale: Multilang::IN_PATH do
    get '/supported-hardware', to: redirect { |params, _request|
      prefix = params[:locale].present? ? "/#{params[:locale]}" : ''
      "#{prefix}/supported-hardware/featured"
    }
  end
  # MARKED FOR DELETION, not before 2026-10-24 (#162).
  #
  # The catalogue lists are served from the static bundle now. They stay for
  # thirty days for the same reason the marketing pages did: try_files walks
  # past a missing file and ends at @rails, so a bundle that is rolled back or
  # half-installed still answers these addresses, and the cutover stays
  # reversible by a symlink flip.
  #
  # What goes with them when they go: Cameras::SocsController#featured and
  # #full_list, app/views/cameras/socs/index.html.erb, _soc.html.erb,
  # _stages.html.erb and PagesHelper's installable_counts/vendor_totals. Not
  # #show and not #index -- the wizard is #163's to move, and the vendor tabs
  # in the bundle link to it.
  scope '(:locale)', locale: Multilang::IN_PATH do
    get '/supported-hardware/featured', to: 'cameras/socs#featured'
    get '/supported-hardware/full-list', to: 'cameras/socs#full_list'
  end

  # What the prerendered hardware pages cannot know (#162).
  #
  # Availability is a question about what upstream has published, answered from
  # a release index a cron refreshes hourly -- so it is the one column of those
  # pages that would go stale between builds. Locale-free by design: these are
  # states, and the page already has the words for them.
  #
  # Outside the locale scope, like the rest of /api/.
  get '/api/v1/hardware/availability.json',
      to: 'api/v1/hardware#availability', defaults: { format: :json }

  # The home page mosaic's tiles and its grant, for the prerendered home pages
  # (#165). Outside the locale scope like the rest of /api/: these are ids and
  # a signed permission, not sentences.
  get '/api/v1/wall/mosaic.json',
      to: 'api/v1/wall#mosaic', defaults: { format: :json }

  # The wall's own pages, as data (#165). One address per page rather than one
  # address with parameters, because both vhosts cache on $uri: a query string
  # that changes the body -- and the grant with it -- would be served to
  # whoever asked next for anything else at the same path.
  #
  # `:id` is constrained to the public-id shape so that the literal `.json`
  # cannot be swallowed as a format, and so that a numeric id reaches the
  # action to be answered 410 rather than being routed away as a mismatch.
  wall_id = /[0-9a-f]{20}|[0-9]+/
  get '/api/v1/wall/page/:page.json',
      to: 'api/v1/wall#page', defaults: { format: :json }, constraints: { page: /\d+/ }
  get '/api/v1/wall/snapshot/:id.json',
      to: 'api/v1/wall#snapshot', defaults: { format: :json }, constraints: { id: wall_id }
  get '/api/v1/wall/snapshot/:id/archive.json',
      to: 'api/v1/wall#archive', defaults: { format: :json }, constraints: { id: wall_id }
  get '/api/v1/wall/snapshot/:id/slideshow.json',
      to: 'api/v1/wall#slideshow', defaults: { format: :json }, constraints: { id: wall_id }

  # /tools/bandwidth-calculator is deliberately absent. It routed to
  # pages#bandwidth_calculator, which has never existed -- no action, no
  # template -- so every request raised AbstractController::ActionNotFound and
  # answered 500 in production. Falling through to the catch-all sends the
  # visitor to the homepage, which is at least a page.
  scope '(:locale)', locale: Multilang::IN_PATH do
    get '/tools/firmware-partitions-calculation', to: 'pages#firmware_partitions_calculation'
    get '/tools/high-resolution-timer', to: 'pages#high_resolution_timer'
    get '/tools/qr-code-generator', to: 'pages#qr_code_generator'
  end
  # /tools/timelaps-interval-calculator is deliberately absent, for the same
  # reason as the bandwidth calculator above: pages#timelaps_interval_calculator
  # has no action and no template under that name, so the route has answered 500
  # for its whole life. app/views/pages/timelaps-interval-calculator.html.erb is
  # left in the tree -- it is an unfinished draft with hardcoded English and no
  # calculation, and finishing it is a decision for whoever started it, not
  # something to be done by a routing change.

  # Commented out in ed0e025, a bulk tidy-up, while five places that redirect to
  # /open-wall were left in: snapshots_controller.rb twice,
  # admin/snapshots_controller.rb, and the breadcrumb on three views. Every one
  # of them fell through to the catch-all and answered a 302 to the homepage.
  #
  # This exposes nothing new. `resources :snapshots` has served the same gallery
  # at /snapshots throughout; these are the URLs the site itself uses for it.
  # The gallery index and the per-snapshot pages are both localized now.
  #
  # `scope '(:locale)'` puts :locale FIRST among a route's dynamic segments,
  # and Rails fills positional helper arguments in segment order -- so inside
  # the scope `snapshot_path(@snapshot)` bound the Snapshot to :locale and
  # raised "missing required keys: [:id]". That is why these two blocks sat
  # outside it. The 26 call sites are now keyword form, so they do not.
  #
  # /open-wall is the address the navbar, the footer and the sitemap use, so it
  # is the one that has to exist in three languages.
  # All three Open Wall entry points, not just the one the navbar uses. The
  # gallery index was localized first because it is what the navbar, the footer
  # and the sitemap link to; leaving the other two behind meant a reader who
  # paged through the wall in Russian, or opened one camera, was back to
  # depending on the session and the browser header for their language -- which
  # is the thing #154 exists to end.
  scope '(:locale)', locale: Multilang::IN_PATH do
    get '/open-wall/camera/:id', to: 'snapshots#camera', as: 'openwall_camera'
    get '/open-wall', to: 'snapshots#index', as: 'open_wall'
    get '/open-wall(/:page)', to: 'snapshots#index'
  end

  # No route here returns image bytes, and that is the point.
  #
  # Three did until 2026-09-23, and between them they were the largest theft on
  # the site. `download` handed over the ORIGINAL upload -- full resolution,
  # EXIF unstripped -- and in the 24 hours to that date it was asked for 18,007
  # times, served 5,825 times, 1,182 MB, covering 2,341 of the 2,820 snapshots
  # that existed. 83% of every frame on the wall. 7,296 of those requests
  # carried the Azure fleet's user agent and 14 of the 10,630 requesting
  # addresses had ever executed the page beacon. robots.txt disallowed the path
  # throughout.
  #
  # `get :camera, on: :collection` was the quiet one: it made
  # /snapshots/camera.jpg?id=<token> a SECOND address for the per-camera JPEG,
  # unlinked and undocumented, so any guard written for the member route missed
  # it entirely. The member route itself (openwall_camera, below) survives as
  # an HTML permalink to a camera; what it no longer does is answer .jpg.
  #
  # Frames reach a reader over the wall transport, which can count what a
  # session has taken. A static URL cannot.
  scope '(:locale)', locale: Multilang::IN_PATH do
    resources :snapshots do
      # The rest of a camera's day, on its own address. Both are what the show
      # page and the slideshow page load into a lazy turbo-frame instead of
      # writing 96 sibling ids into every render (#261); both stay reachable
      # without JavaScript, which is why they are routes and not a format.
      get :archive, on: :member
      get :oneday, on: :member
      get :slideshow, on: :member
    end
  end

  # The catalogue is the part of the site with real long-tail search value, so
  # it is the part that most wanted a language in its address.
  #
  # Every rule protecting these paths lives in an nginx location anchored at
  # ^/, and a rule simply stops applying to a path its regex does not match.
  # Localizing this block without the matching (ru|zh)/ prefixes in the vhost
  # would have put /ru/cameras/.../download_full_image -- about a second of CPU
  # and 8-32MB of disk per call -- outside the #147 limit_req zones. #205 added
  # those prefixes first, and a test now fails if the two lists drift apart.
  # `cameras/vendors#show` is MARKED FOR DELETION, not before 2026-10-24
  # (#162): the vendor tabs are in the bundle now, and this stays thirty days
  # as the fallback behind try_files. The rest of this block is the wizard and
  # the download, which are #163's and never leave Rails respectively.
  scope '(:locale)', locale: Multilang::IN_PATH do
    namespace :cameras do
      resources :socs, only: %i[index show]
      resources :vendors, only: %i[index show] do
        # No :update. The wizard's result is a GET on the SoC's own address
        # since #156 -- `?camera[...]` renders the instructions, bare renders
        # the form -- so there is one address per SoC and one way in. The PUT
        # persisted nothing and only cost the form an authenticity_token, which
        # was the last thing writing a session on a public page.
        resources :socs, only: %i[index show] do
          get :download_full_image, on: :member
        end
      end
    end
  end

  devise_for :admin
  namespace :admin do
    resources :snapshots
    resources :socs
    resources :vendors
  end
  as :admin do
    get "/admin", to: "admin/dashboard#show", as: "admin_root"
    get "/admin/sign_out", to: "devise/sessions#destroy"
  end

  # Retired 2026-08. Falling through to the catch-all below would answer
  # 302-then-200 at the homepage, which for a page whose remaining traffic is
  # entirely scripted is a lie -- the fortnight before it went, 25 of its 26
  # fetches carried a curl or Wget agent. 410 tells them to stop asking.
  match "/binaries", to: proc { [410, { "Content-Type" => "text/plain" }, ["Gone\n"]] },
        via: :all, as: :retired_binaries

  # Same treatment, same reason. /telemetry was a shortcut to
  # github.com/OpenIPC/telemetry, which does not exist and by all appearances
  # never has, so it had been bouncing visitors to GitHub's own 404. Deleting
  # the route is worse rather than better: the catch-all answers unknown paths
  # with a 302 to the homepage, which tells a crawler the page moved there.
  match "/telemetry(/*any)", to: proc { [410, { "Content-Type" => "text/plain" }, ["Gone\n"]] },
        via: :all, as: :retired_telemetry

  # ActiveStorage's public routes, refused.
  #
  # This used to be the opposite: a constraint excluding anything containing
  # 'rails/active_storage' from the catch-all, added in 2022 so those requests
  # would fall through to the engine instead of being redirected away. That
  # made /rails/active_storage/representations/redirect/... and
  # /rails/active_storage/disk/... live public addresses for the ORIGINAL
  # uploads and every variant -- reachable for as long as the signed id lasts,
  # which for a blob id is forever.
  #
  # Snapshot is the only model on this site with an attachment, and nothing
  # renders an ActiveStorage URL any more, so the engine's routes serve no
  # purpose here beyond being a door. 410 rather than 404: they existed, they
  # are gone, and a crawler holding an old link should stop asking.
  match "/rails/active_storage/*any",
        to: proc { [410, { "Content-Type" => "text/plain" }, ["Gone\n"]] },
        via: :all, as: :retired_active_storage

  match "*unmatched", to: "application#route_not_found", via: :all
end
