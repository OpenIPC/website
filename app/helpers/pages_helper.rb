# frozen_string_literal: true

# Helpers for the static marketing pages: the page title, and the partner wall.
module PagesHelper
  # The partner wall, in Ruby rather than hardcoded into a template.
  #
  # Both lists were transcribed from the wall on /introduction as it stands
  # today, not from an older copy: entries that page has commented out are
  # commented out here too, with the same URLs, so nothing appears or disappears
  # silently when /introduction is eventually retired. Uncommenting a line here
  # is how one comes back.
  #
  # Shown to every visitor.
  INTERNATIONAL_PARTNERS = [
    { name: 'Open Source Collective', url: 'https://www.oscollective.org/', img: 'partners/osc_mini.png' },
    { name: 'GitHub',         url: 'https://github.com/',                 img: 'partners/github_mini.png' },
    { name: 'RunCam',         url: 'https://runcam.com/',                 img: 'partners/runcam_mini.png' },
    { name: 'CCDCAM',         url: 'https://ccdcam.com/',                 img: 'partners/ccdcam_mini.png' },
    { name: 'wfb-ng',         url: 'https://github.com/svpcom/wfb-ng/',   img: 'partners/wfb-ng_mini.png' },
    { name: 'RubyFPV',        url: 'https://rubyfpv.com/',                img: 'partners/rubyfpv_mini.png' },
    { name: 'Mario FPV',      url: 'https://www.youtube.com/@mariofpv',   img: 'partners/mariofpv_mini.png' },
    { name: 'Linux Chenxing', url: 'https://linux-chenxing.org/',         img: 'partners/linuxchenxing_mini.png' },
    { name: 'TUDSaT',         url: 'https://www.tudsat.space/',           img: 'partners/tudsat_mini.png' },
    { name: 'WüSpace',        url: 'https://wuespace.de/',                img: 'partners/wuespace_mini.png' },
    { name: 'Really',         url: 'https://opencollective.com/really-541ee976', img: 'partners/really_mini.png' }
    # Commented out on /introduction, so commented out here:
    # { name: 'EMAX',    url: 'https://emaxmodel.com/', img: 'partners/emax_mini.png' },
    # { name: 'GoodCam', url: 'https://www.goodcam.io/', img: 'partners/goodcam_mini.png' },
    #
    # partners/baresip_mini.png exists as an asset but has never been on the
    # wall. Left off rather than introduced by a refactor.
  ].freeze

  # Integrators are territory-specific: these serve Russia and are shown only to
  # Russian-language visitors. Showing them to everyone was explicitly not
  # wanted, and the reverse -- gating them in CSS on /introduction with
  # `html:not([lang="ru"])` -- never worked, because no logo on that page ever
  # carried the `ru` class the rule selects on.
  #
  # NOTE FOR REVIEW: this whole block is currently commented out on
  # /introduction, so no visitor sees any of it today. Rendering it for :ru
  # brings it back. That is what the relaunch plan asks for, but it is a content
  # decision rather than a technical one -- say so if it should stay hidden.
  RU_INTEGRATORS = [
    { name: 'GoodCam',       url: 'https://www.goodcam.io/',      img: 'partners/goodcam_mini.png' },
    { name: 'SkyCam',        url: 'https://skycam.cam/',          img: 'partners/skycam_mini.png' },
    { name: 'Vixand',        url: 'https://vixand.ru/',           img: 'partners/vixand_mini.png' },
    { name: 'Improve IT',    url: 'https://3it.ru/',              img: 'partners/improve_mini.png' },
    { name: 'UfaNet',        url: 'https://www.ufanet.ru/',       img: 'partners/ufanet_mini.png' },
    { name: 'Dvor24',        url: 'https://dvor24.ru/',           img: 'partners/dvor24_mini.png' },
    { name: 'Sputnik',       url: 'https://sputnik.systems/',     img: 'partners/sputnik_mini.png' },
    { name: 'Techno-Shield', url: 'https://msvoko.ru/',           img: 'partners/techno-shield_mini.png' },
    { name: 'KeyTelecom',    url: 'https://keytele.com/',         img: 'partners/keytelecom_mini.png' },
    { name: 'AnyCam',        url: 'https://anycam.io/',           img: 'partners/anycam_mini.png' },
    { name: 'WebGlazok',     url: 'https://webglazok.com/',       img: 'partners/webglazok_mini.png' },
    { name: 'Yucca',         url: 'https://yucca.app/en',         img: 'partners/yucca_mini.png' },
    { name: 'IPEYE',         url: 'https://ipeye.ru/',            img: 'partners/ipeye_mini.png' },
    { name: 'VTL',           url: 'https://vtl.su/#rec35109538',  img: 'partners/vtl_mini.png' },
    { name: 'S-Video',       url: 'https://www.cctvsp.ru/cctv/openipc', img: 'partners/s-video_mini.png' },
    { name: 'MyWiFi',        url: 'https://xn--80aaaf0bh2e7a5c.xn--p1ai/', img: 'partners/mywifi-cc_mini.png' },
    { name: 'AlarmSystem',
      url: 'https://alarmsystem-cctv.ru/product-category/cctv-products/cctv-cameras/ip-cameras-cctv/' \
           '?swoof=1&product_brands=openipc&really_curr_tax=189-product_cat',
      img: 'partners/alarmsystem_mini.png' }
    # Commented out on /introduction, so commented out here:
    # { name: 'MegaCam',          url: 'https://megacam.kz/',           img: 'partners/megacam_mini.png' },
    # { name: 'Dozor',            url: 'https://dozor-smart.ru/',       img: 'partners/dozor_mini.png' },
    # { name: 'Flagman',          url: 'https://flagman.org/',          img: 'partners/flagman_mini.png' },
    # { name: 'Meldana',          url: 'https://meldana.com/',          img: 'partners/meldana_mini.png' },
    # { name: 'Binary Machines',  url: 'https://bmachines.ru/',         img: 'partners/binary-machines_mini.png' },
    # { name: 'Expo Electronica', url: 'https://expoelectronica.ru/en/', img: 'partners/expo-electronica_mini.png' },
    # { name: 'GAINS',            url: 'https://gains.company/',        img: 'partners/gain_mini.png' }
  ].freeze

  # The latency comparison on /low-latency.
  #
  # The table this replaces was unchanged 2022 announcement copy, keyed on
  # resolution -- which is very nearly free. A 2026 audit of the OpenIPC and
  # wfb-ng chat archives found those figures optimistic by 40-160 ms at the
  # exact configurations they named, and simultaneously understating the floor
  # by half. What actually decides the number is the receive path, so that is
  # what these four bars compare.
  #
  # low and high are the lowest and highest figures users report for each path,
  # not an average: collapsing a path to one number is how the old table came to
  # promise something nobody could reach. The audit itself carries the
  # per-report detail, which is a wiki subject rather than a landing-page one.
  #
  # Scale is fixed rather than derived from the data so the bars stay comparable
  # if a figure changes.
  LATENCY_SCALE_MAX = 120

  LATENCY_PATHS = [
    { key: :ground_station, low: 26, high: 67 },
    { key: :goggles,        low: 45, high: 65 },
    { key: :phone,          low: 50, high: 100 },
    { key: :desktop,        low: 60, high: 100 }
  ].freeze

  # Percentage offsets for one bar, against the fixed scale above.
  def latency_bar_style(path)
    left  = path[:low] * 100.0 / LATENCY_SCALE_MAX
    width = (path[:high] - path[:low]) * 100.0 / LATENCY_SCALE_MAX
    "left: #{left.round(1)}%; width: #{width.round(1)}%"
  end

  def page_title
    [@page_title, 'OpenIPC'].join(' - ')
  end

  def partner_logos
    logos = INTERNATIONAL_PARTNERS
    logos += RU_INTEGRATORS if I18n.locale.eql?(:ru)
    logos
  end
end
