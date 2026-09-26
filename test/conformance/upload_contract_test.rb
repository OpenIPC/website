# frozen_string_literal: true

require_relative 'conformance_helper'

# POST /snapshots, the one request on this site whose client cannot be updated
# (#291). Cameras in the field run firmware nobody can reach, so what this
# endpoint answers is frozen: the status, the header names, the exact X-Error
# sentences, the arithmetic in Retry-After -- including the parts that look
# like defects, which are pinned as defects and said to be.
#
# Most of these store nothing. A refusal writes no row, and a verdict on a file
# can be read without accepting it: sent with a MAC the server will refuse, the
# refusal still lists every file error, because validation does not stop at the
# first failure. Only the tests that must see an acceptance write, and they
# need CONFORMANCE_DATABASE_URL so they can delete what they wrote.
class UploadContractConformanceTest < Conformance::Case
  PROBE_MAC = 'not-a-mac'
  MAC_INVALID = 'MAC address is invalid'
  NO_FILE = "File can't be blank"
  TOO_SMALL = 'File File size should be greater than 10 KB'
  TOO_LARGE = 'File File size should be less than 5 MB'

  def jpeg(size)
    { filename: 'snapshot.jpg', content_type: 'image/jpeg', data: Conformance::Images.jpeg(size) }
  end

  def assert_refused(response, status, x_error, context)
    assert_equal status.to_s, response.code, "#{context}: X-Error #{response['X-Error'].inspect}"
    assert_equal x_error, response['X-Error'], context
    assert_empty_body response, context
  end

  # --- X-Error, exactly ----------------------------------------------------
  #
  # The body is errors.full_messages.join('. '): sentences in the order the
  # model declares its validations -- the file, then the MAC -- joined by a
  # full stop and a space, and no full stop after the last. A camera that logs
  # this header logs these bytes.

  test 'no file and no MAC: three sentences, in declaration order' do
    assert_refused upload(mac: nil, file: false), 415,
                   "#{NO_FILE}. MAC address can't be blank. #{MAC_INVALID}", 'neither'
  end

  test 'no file: one sentence' do
    assert_refused upload(mac: fresh_mac, file: false), 415, NO_FILE, 'no file'
  end

  test 'a malformed MAC: one sentence' do
    assert_refused upload(mac: PROBE_MAC), 415, MAC_INVALID, 'bad MAC'
  end

  # --- the size rule, at its four edges -------------------------------------
  #
  # 10 KB and 5 MB inclusive. "File File size" is what the validator gem says,
  # doubled word and all; a port that tidies it changes the header.

  test 'one byte under the floor is refused, the floor itself is not' do
    assert_refused upload(mac: fresh_mac, file: jpeg(10_239)), 415, TOO_SMALL, '10239 bytes'
    assert_refused upload(mac: PROBE_MAC, file: jpeg(10_240)), 415, MAC_INVALID, '10240 bytes'
  end

  # The application's ceiling. A camera has never met it: see the next test.
  test 'the ceiling itself is accepted, one byte over is not' do
    skip 'nginx refuses these bodies first; see the 1 MB test' unless Conformance.direct?

    assert_refused upload(mac: PROBE_MAC, file: jpeg(5_242_880)), 415, MAC_INVALID, '5242880 bytes'
    assert_refused upload(mac: fresh_mac, file: jpeg(5_242_881)), 415, TOO_LARGE, '5242881 bytes'
  end

  # The ceiling the fleet actually has. The upload's nginx location carries
  # client_max_body_size 1m -- written out on 2026-09-25, but nginx's default
  # before that, so production has always answered a body over 1 MB with 413
  # before Rails saw it, and the validator's 5 MB has never been reachable
  # from outside. Found writing this suite, 2026-09-26: an upload between 1 MB and
  # 5 MB is refused, not stored. A port behind this vhost inherits it; a port
  # that is reached some other way must decide on purpose.
  test 'through nginx, a body over 1 MB is refused before the application' do
    skip 'the vhost answers this, not the application' if Conformance.direct?

    assert_equal '415', upload(mac: PROBE_MAC, file: jpeg(900_000)).code, 'under 1 MB reaches the application'
    assert_equal '413', upload(mac: PROBE_MAC, file: jpeg(1_100_000)).code, 'over 1 MB is nginx\'s to refuse'
  end

  # --- where the request may arrive, and what may not happen to it ----------

  # `scope '(:locale)'` wraps `resources :snapshots`, so the prefixed address is
  # the same endpoint. Nothing is known to post there; everything must keep
  # answering it as the upload, not as a 404 or a page.
  #
  # And in Russian, half-way. The locale translates the attribute name and not
  # the sentence, so X-Error reads "MAC-адрес is invalid" -- and those are raw
  # UTF-8 bytes in an HTTP header, which is not ASCII and not RFC-encoded.
  # Found by this suite on its first run, 2026-09-26. Pinned as the defect it
  # is: a port that answers in plain English here is a change, and so is one
  # that answers in Russian.
  MAC_INVALID_RU = "MAC-\u0430\u0434\u0440\u0435\u0441 is invalid".b

  def assert_russian_refusal(response, context)
    assert_equal '415', response.code, context
    assert_equal MAC_INVALID_RU, response['X-Error'].to_s.b, context
    assert_empty_body response, context
  end

  test 'POST /ru/snapshots is the same endpoint, and answers half in Russian' do
    assert_russian_refusal upload(mac: PROBE_MAC, path: '/ru/snapshots'), '/ru/snapshots'
  end

  # LocaleRedirect answers ?locale=xx with a 301 -- on GET only. A redirect
  # here would lose the body, and a camera's HTTP client does not follow one
  # with its POST intact. One boolean away from bricking the fleet. The
  # parameter does pick the locale, as the prefix does.
  test 'a POST carrying ?locale=ru is not redirected' do
    assert_russian_refusal upload(mac: PROBE_MAC, path: '/snapshots?locale=ru'), '?locale=ru'
  end

  # No cookie, no token, no Referer, and an Origin that is someone else's. The
  # upload must not care: a CSRF refusal would be a redirect in production and
  # an origin check a 403, and either one is every camera refused.
  test 'the upload needs no CSRF token and checks no origin' do
    response = upload(mac: PROBE_MAC, headers: { 'Origin' => 'https://evil.example' })

    assert_refused response, 415, MAC_INVALID, 'foreign origin'
  end

  # --- the blacklist ----------------------------------------------------------

  # 403 comes from an exception raised inside a validation, not a validation
  # failure, so it bypasses the 415 path: no X-Error, and it wins even over a
  # missing file.
  test 'a blacklisted MAC is refused with a bare 403' do
    mac = ENV['CONFORMANCE_BLACKLISTED_MAC'].to_s
    skip 'needs CONFORMANCE_BLACKLISTED_MAC, a MAC the server blacklists' if mac.empty?

    [upload(mac: mac), upload(mac: mac, file: false)].each do |response|
      assert_equal '403', response.code
      assert_nil response['X-Error'], 'a blacklisted camera is told nothing'
      assert_empty_body response, 'blacklisted'
    end
  end

  # --- acceptance -------------------------------------------------------------

  test 'an accepted upload answers 201 with a relative Location and no body' do
    needs_database!
    mac = fresh_mac

    response = upload(mac: mac, file: jpeg(10_240))

    assert_equal '201', response.code, response['X-Error']
    assert_match(%r{\A/snapshots/[0-9a-f]{20}\z}, response['Location'],
                 'Location is a path to the opaque id, never a URL and never the row id')
    assert_empty_body response, 'created'
    assert_equal 1, database.snapshots_from(mac)
  end

  test 'the prefixed address and a foreign origin are accepted too' do
    needs_database!

    [{ path: '/ru/snapshots' }, { headers: { 'Origin' => 'https://evil.example' } },
     { path: '/snapshots?locale=ru' }].each do |options|
      mac = fresh_mac
      response = upload(mac: mac, **options)

      assert_equal '201', response.code, "#{options}: #{response['X-Error']}"
      assert_equal 1, database.snapshots_from(mac), options.to_s
    end
  end

  test 'the ceiling is accepted, not only unobjected to' do
    needs_database!
    skip 'nginx refuses these bodies first' unless Conformance.direct?
    mac = fresh_mac

    assert_equal '201', upload(mac: mac, file: jpeg(5_242_880)).code
    assert_equal 1, database.snapshots_from(mac)
  end

  test 'a refusal writes nothing' do
    needs_database!
    mac = fresh_mac

    upload(mac: mac, file: jpeg(10_239))
    upload(mac: mac, file: false)

    assert_equal 0, database.snapshots_from(mac)
  end

  # --- the interval -----------------------------------------------------------
  #
  # One frame per camera per fifteen minutes, with two minutes of hysteresis:
  # the gate opens at 13:00 (780 s), because a quarter-hour cron drifts.
  #
  # Retry-After is INTERVAL_LIMIT - elapsed and does NOT subtract the
  # hysteresis, so it over-reports by 120 s: at elapsed 0 it says 900 while the
  # gate opens at 780. That is a defect, pinned as one. A port written from the
  # model will "fix" it, and a camera that waits exactly what it was told would
  # not notice -- but anything that reads the number would.

  def refused_after(elapsed)
    mac = fresh_mac
    database.seed_frame(mac, elapsed)
    response = upload(mac: mac)
    [response, mac]
  end

  test 'Retry-After is fifteen minutes less the time since the last frame' do
    needs_database!

    { 0 => 900, 300 => 600, 779 => 121 }.each do |elapsed, expected|
      response, mac = refused_after(elapsed)

      assert_equal '429', response.code, "elapsed #{elapsed}"
      assert_in_delta expected, Integer(response['Retry-After']), 2, "Retry-After at elapsed #{elapsed}"
      assert_nil response['X-Error'], '429 carries Retry-After, not X-Error'
      assert_empty_body response, "429 at #{elapsed}"
      assert_equal 1, database.snapshots_from(mac), 'a refused frame was stored'
    end
  end

  # The two rows that ARE the hysteresis constant.
  test 'the gate is shut at 779 seconds and open at 780' do
    needs_database!

    shut, = refused_after(779)
    assert_equal '429', shut.code, 'refused one second early is the rule; accepted means the hysteresis grew'

    mac = fresh_mac
    database.seed_frame(mac, 780)
    open = upload(mac: mac)
    assert_equal '201', open.code, "780 s is thirteen minutes: #{open['X-Error']}"
    assert_equal 2, database.snapshots_from(mac)
  end

  test 'the interval is per camera' do
    needs_database!
    database.seed_frame(fresh_mac, 0)

    assert_equal '201', upload(mac: fresh_mac).code
  end

  # The whitelist exempts an ADDRESS from the interval. Reachable only when the
  # suite talks to the application directly, from loopback: there Rails trusts
  # X-Forwarded-For the way it trusts nginx's. Through nginx the client does
  # not choose its own address, which is the point.
  test 'a whitelisted address is exempt from the interval' do
    needs_database!
    address = ENV['CONFORMANCE_WHITELISTED_IP'].to_s
    skip 'needs CONFORMANCE_WHITELISTED_IP, an address the server whitelists' if address.empty?
    skip 'only reachable talking to the application directly' unless Conformance.direct?

    mac = fresh_mac
    database.seed_frame(mac, 0)

    assert_equal '201', upload(mac: mac, headers: { 'X-Forwarded-For' => address }).code
    assert_equal '429', upload(mac: mac, headers: { 'X-Forwarded-For' => '198.51.100.200' }).code,
                 'the exemption is for the address, not the camera'
  end
end
