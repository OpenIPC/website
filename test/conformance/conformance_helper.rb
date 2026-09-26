# frozen_string_literal: true

require 'active_support'
require 'active_support/test_case'
require 'active_support/core_ext/object/blank'
require 'json'
require 'net/http'
require 'open3'
require 'openssl'
require 'securerandom'
require 'socket'
require 'uri'

# A black-box conformance suite for the surface Rails still owns (#291).
#
# Everything here talks to a base URL and nothing talks to the app in-process,
# so the same suite can be pointed at Rails today, at the Go service tomorrow
# (#292 onwards), and at either one through nginx -- which is where several of
# these behaviours actually live. It does not load the Rails environment; what
# it knows about Rails it knows from the fixtures under fixtures/, which Rails
# wrote (`bin/rails conformance:fixtures`).
#
#   CONFORMANCE_BASE_URL       required; the suite defines no tests without it
#   CONFORMANCE_HOST           Host header, when the base URL is an address
#   CONFORMANCE_DATABASE_URL   the server's database. With it, the suite may
#                              write -- accept an upload, seed a camera's last
#                              frame -- and cleans up after itself. Without it,
#                              only what stores nothing runs.
#   CONFORMANCE_BLACKLISTED_MAC / CONFORMANCE_WHITELISTED_IP
#                              what the server was told (SNAPSHOT_MAC_BLACKLIST,
#                              SNAPSHOT_IP_WHITELIST); those tests skip without
#   CONFORMANCE_SURFACES       which surfaces the server answers, comma-separated
#                              (upload, wall, read). Unset means all of them,
#                              which is Rails; the Go service answers upload and
#                              wall, so bin/conformance --target go names those.
#
# The database URL's scheme picks the adapter: mysql2:// for Rails' MariaDB,
# postgres:// for the Go service's PostgreSQL (#293). Both keep the snapshots
# table's column names, which is why the SQL layer transfers at all.
#
# bin/conformance boots a server with all of these set and runs the suite.
#
# Three layers, because they fail differently:
#   1. Net::HTTP     status, parsed headers, body -- most assertions
#   2. a raw socket  the upload path only. Net::HTTP normalises header names;
#                    a camera reading a raw buffer does not
#   3. SQL           what an upload wrote. These transfer to a port only if it
#                    keeps the table's shape, which is a reason to (#293)
module Conformance
  # Locally administered (the 02 bit), and a prefix nobody's camera has: every
  # row the suite writes carries one, which is how it finds them to delete.
  MAC_PREFIX = '02:c0:f0'

  class << self
    def enabled?
      ENV['CONFORMANCE_BASE_URL'].to_s.strip != ''
    end

    def base
      @base ||= URI(ENV.fetch('CONFORMANCE_BASE_URL').chomp('/'))
    end

    def host_header
      ENV['CONFORMANCE_HOST'].presence || base.host
    end

    # Talking to the application directly rather than through nginx, the suite
    # has to say what nginx would have said: that the request arrived over TLS.
    # Production forces SSL and would otherwise answer every request 301.
    def direct?
      base.scheme == 'http'
    end

    def database?
      ENV['CONFORMANCE_DATABASE_URL'].to_s.strip != ''
    end

    # A test class that exercises one surface runs only when the server under
    # test answers it.
    def surface?(name)
      list = ENV['CONFORMANCE_SURFACES'].to_s.split(',').map(&:strip).reject(&:empty?)
      list.empty? || list.include?(name.to_s)
    end

    def fixture(name)
      JSON.parse(File.read(File.join(__dir__, '..', '..', 'service', 'conformance', 'testdata', "#{name}.json")))
    end
  end

  # A JPEG of an exact size: SOI, a JFIF APP0, comment segments to the length
  # asked for, EOI. The same construction as snapshots_controller_test, for the
  # same reason -- the size bounds are the subject, so the size must be exact.
  module Images
    JPEG_HEAD = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00".b
    JPEG_TAIL = "\xFF\xD9".b
    MAX_COMMENT = 65_533

    module_function

    def jpeg(size)
      budget = size - JPEG_HEAD.bytesize - JPEG_TAIL.bytesize
      JPEG_HEAD + payloads(budget).map { |payload| comment(payload) }.join.b + JPEG_TAIL
    end

    # Comment payload sizes filling `budget` exactly, four bytes of marker and
    # length each; a 16-bit length cannot describe more than 64 KB.
    def payloads(budget)
      count = (budget.to_f / (MAX_COMMENT + 4)).ceil
      base, extra = (budget - (4 * count)).divmod(count)
      Array.new(count) { |i| base + (i < extra ? 1 : 0) }
    end

    def comment(payload)
      "\xFF\xFE".b + [payload + 2].pack('n') + ("\x20".b * payload)
    end

    # Pads a byte prefix out to a size the size rule accepts, so a verdict
    # about the bytes is not also a verdict about the length.
    def padded(prefix, size = 12_288)
      prefix.b + ("\x00".b * [size - prefix.bytesize, 0].max)
    end
  end

  # multipart/form-data, built by hand so every layer sends the same bytes.
  module Multipart
    module_function

    def build(fields, file: nil)
      boundary = "conformance#{SecureRandom.hex(12)}"
      parts = fields.map { |name, value| field_part(boundary, name, value) }
      parts << file_part(boundary, file) if file
      ["multipart/form-data; boundary=#{boundary}", "#{parts.join.b}--#{boundary}--\r\n".b]
    end

    def field_part(boundary, name, value)
      "--#{boundary}\r\nContent-Disposition: form-data; name=\"#{name}\"\r\n\r\n#{value}\r\n".b
    end

    # No Content-Type line at all when none is declared, which is what a
    # client that does not know the type sends.
    def file_part(boundary, file)
      head = "--#{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"#{file[:filename]}\"\r\n"
      head += "Content-Type: #{file[:content_type]}\r\n" if file[:content_type]
      "#{head}\r\n".b + file[:data].b + "\r\n".b
    end
  end

  # The server's database, for what an HTTP answer cannot show.
  module Database
    def self.new(url = ENV.fetch('CONFORMANCE_DATABASE_URL'))
      url.start_with?('postgres') ? Postgres.new(url) : MySQL.new(url)
    end

    class MySQL
      def initialize(url)
        require 'mysql2'
        uri = URI(url)
        @client = Mysql2::Client.new(host: uri.host, port: uri.port || 3306, username: uri.user,
                                     password: uri.password, database: uri.path.delete_prefix('/'))
      end

      def snapshots_from(mac)
        @client.query("SELECT COUNT(*) AS n FROM snapshots WHERE mac_address = '#{@client.escape(mac)}'")
               .first['n']
      end

      # The camera's last frame, `seconds` ago by the database's clock -- the
      # clock the server compares against, which the test machine's may not be.
      def seed_frame(mac, seconds_ago)
        @client.query(<<~SQL)
          INSERT INTO snapshots (mac_address, ip_address, created_at, updated_at)
          VALUES ('#{@client.escape(mac)}', '192.0.2.1',
                  UTC_TIMESTAMP(6) - INTERVAL #{Integer(seconds_ago)} SECOND,
                  UTC_TIMESTAMP(6) - INTERVAL #{Integer(seconds_ago)} SECOND)
        SQL
      end

      def delete_from(mac)
        @client.query("DELETE FROM snapshots WHERE mac_address = '#{@client.escape(mac)}'")
      end
    end

    # Through the psql client rather than a gem, so the Gemfile does not change
    # for a test helper. The seeded row carries the columns the Go schema
    # requires and Rails' does not: a public id, a camera token, a size.
    class Postgres
      def initialize(url)
        @url = url
      end

      def snapshots_from(mac)
        Integer(query("SELECT count(*) FROM snapshots WHERE mac_address = #{quote(mac)}"))
      end

      def seed_frame(mac, seconds_ago)
        id = "#{SecureRandom.hex(9)}#{%w[a b c d e f].sample}0"
        query(<<~SQL)
          INSERT INTO snapshots (public_id, mac_address, camera_token, ip_address, content_type, byte_size, created_at)
          VALUES ('#{id}', #{quote(mac)}, 'seed', '192.0.2.1', 'image/jpeg', 1,
                  now() - make_interval(secs => #{Integer(seconds_ago)}))
        SQL
      end

      def delete_from(mac)
        query("DELETE FROM snapshots WHERE mac_address = #{quote(mac)}")
      end

      private

      def quote(value)
        "'#{value.to_s.gsub("'", "''")}'"
      end

      def query(sql)
        out, status = Open3.capture2e('psql', @url, '-X', '-q', '-t', '-A', '-v', 'ON_ERROR_STOP=1', '-c', sql)
        raise "psql failed: #{out}" unless status.success?

        out.strip
      end
    end
  end

  class Case < ActiveSupport::TestCase
    # Nothing to run without a server, and a run of the whole suite has none.
    # Defining no tests keeps that run's skip count meaning something.
    def self.runnable_methods
      Conformance.enabled? && Conformance.surface?(surface) ? super : []
    end

    # The surface a test class exercises; see Conformance.surface?.
    def self.surface(name = nil)
      @surface = name if name
      @surface || (superclass.respond_to?(:surface) ? superclass.surface : :upload)
    end

    # The server's rows are not in a transaction this process can roll back.
    self.use_transactional_tests = false if respond_to?(:use_transactional_tests=)

    teardown do
      Array(@macs).each { |mac| database.delete_from(mac) } if Conformance.database?
    end

    # A camera nobody else in this run is using, so no two tests share an
    # interval, and teardown can find every row this one wrote.
    def fresh_mac
      mac = "#{MAC_PREFIX}:#{SecureRandom.hex(3).scan(/../).join(':')}"
      (@macs ||= []) << mac
      mac
    end

    def database
      @database ||= Database.new
    end

    def needs_database!
      skip 'needs CONFORMANCE_DATABASE_URL: this test writes a row it must delete' unless Conformance.database?
    end

    def http(method, path, body: nil, headers: {})
      uri = URI.join("#{Conformance.base}/", path.delete_prefix('/'))
      request = build_request(method, uri, body, headers)
      Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == 'https', open_timeout: 10, read_timeout: 60) do |c|
        c.request(request)
      end
    end

    def build_request(method, uri, body, headers)
      request = Net::HTTP.const_get(method.to_s.capitalize).new(uri)
      default_headers.merge(headers).each { |name, value| request[name] = value }
      request.body = body if body
      request
    end

    def get(path, headers: {})
      http(:get, path, headers: headers)
    end

    def default_headers
      headers = { 'Host' => Conformance.host_header, 'User-Agent' => 'openipc-conformance' }
      headers['X-Forwarded-Proto'] = 'https' if Conformance.direct?
      headers
    end

    # The upload, the way a camera sends it. `file: false` sends none.
    def upload(mac:, file: nil, path: '/snapshots', headers: {}, **fields)
      fields = { mac_address: mac, soc: 'gk7205v300', sensor: 'imx307', firmware: 'lite',
                 streamer: 'majestic' }.merge(fields).compact
      file = { filename: 'snapshot.jpg', content_type: 'image/jpeg', data: Images.jpeg(12_288) } if file.nil?
      content_type, body = Multipart.build(fields, file: file || nil)
      http(:post, path, body: body, headers: { 'Content-Type' => content_type }.merge(headers))
    end

    # The same request over a bare socket, answering the raw header block as
    # it arrived: names in the case they were sent, lines in their order.
    def raw_upload(mac:, file: nil, **fields)
      fields = { mac_address: mac }.merge(fields)
      file = { filename: 'snapshot.jpg', content_type: 'image/jpeg', data: Images.jpeg(12_288) } if file.nil?
      content_type, body = Multipart.build(fields, file: file || nil)
      head = ['POST /snapshots HTTP/1.1', "Host: #{Conformance.host_header}",
              "Content-Type: #{content_type}", "Content-Length: #{body.bytesize}", 'Connection: close']
      head << 'X-Forwarded-Proto: https' if Conformance.direct?
      raw_exchange("#{head.join("\r\n")}\r\n\r\n".b + body)
    end

    def raw_exchange(bytes)
      socket = open_socket
      socket.write(bytes)
      read_head(socket)
    ensure
      socket&.close
    end

    def open_socket
      socket = TCPSocket.new(Conformance.base.host, Conformance.base.port)
      return socket unless Conformance.base.scheme == 'https'

      context = OpenSSL::SSL::SSLContext.new
      context.set_params(verify_mode: OpenSSL::SSL::VERIFY_PEER)
      OpenSSL::SSL::SSLSocket.new(socket, context).tap do |tls|
        tls.hostname = Conformance.base.host
        tls.sync_close = true
        tls.connect
      end
    end

    # Up to the blank line that ends the header block, and not a byte more.
    def read_head(socket)
      response = +''.b
      response << socket.readpartial(65_536) until response.include?("\r\n\r\n")
      response.split("\r\n\r\n", 2).first
    rescue EOFError
      response.split("\r\n\r\n", 2).first
    end

    # An empty body, said whichever way the answer came: Puma sends
    # Content-Length: 0, nginx re-frames the same answer as a zero-length chunk.
    def assert_empty_body(response, context)
      assert_equal '', response.body.to_s, "#{context}: a body where a camera expects none"
      length = response['Content-Length']
      assert_equal '0', length, "#{context}: Content-Length #{length}" if length
    end
  end
end
