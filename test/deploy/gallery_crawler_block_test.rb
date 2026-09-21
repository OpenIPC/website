# frozen_string_literal: true

require 'test_helper'

# The Open Wall is closed to crawlers, in all four of its spellings.
#
# On 2026-09-21 GPTBot fetched the gallery 12,962 times and took 1.5 GB in
# under two hours -- 18% of everything the site served that day -- without
# breaking a single rule. robots.txt disallowed /snapshots/; the site also
# answers at /ru/snapshots/, /zh/snapshots/ and /wall/, and none of those
# three were named. The locale-in-path work and #146's move of the images onto
# plain files had each added a door, and the file that closes those doors was
# never widened to match.
#
# That is the failure this file exists to catch, and it is silent by
# construction: every request is a 200, nothing errors, and the only symptom
# is a bandwidth bill and other people's cameras in somebody's training set.
# So the assertions below are written as the URLs that were actually fetched,
# not as the patterns that ought to cover them.
class GalleryCrawlerBlockTest < ActiveSupport::TestCase
  ROBOTS = Rails.root.join('public/robots.txt').read.freeze
  CONF = Rails.root.join('deploy/nginx/conf.d/openipc-crawler-block.conf').read.freeze
  VHOST = Rails.root.join('deploy/nginx/sites-available/org.openipc').read.freeze

  # The `Disallow:` rules for `User-agent: *`, as a crawler reads them: a
  # prefix match, with `*` standing for any run of characters. Every major
  # crawler implements the wildcard; the plain prefix is all the standard
  # requires.
  def disallowed?(path)
    ROBOTS.scan(/^Disallow:\s*(\S+)/).flatten.any? do |rule|
      literal = rule.split('*', -1).map { |part| Regexp.escape(part) }.join('.*')
      Regexp.new("\\A#{literal}").match?(path)
    end
  end

  # Four spellings of one gallery. The first is the only one the file had
  # before 2026-09-21; the rest are the ones GPTBot walked through.
  {
    'the unprefixed gallery' => '/snapshots/4a1ffd6d0a3b9c221e7f',
    'the Russian gallery' => '/ru/snapshots/4a1ffd6d0a3b9c221e7f',
    'the Chinese gallery' => '/zh/snapshots/4a1ffd6d0a3b9c221e7f',
    'a day in the life, prefixed' => '/ru/snapshots/4a1ffd6d0a3b9c221e7f/oneday',
    'the original upload' => '/zh/snapshots/4a1ffd6d0a3b9c221e7f/download',
    'a per-camera page' => '/open-wall/camera/4a1ffd6d0a3b9c221e7f',
    'the same page prefixed' => '/zh/open-wall/camera/4a1ffd6d0a3b9c221e7f',
    'the image itself' => '/wall/4a1ffd6d0a3b9c221e7f/fullhd.jpg',
    'the ActiveStorage URL it replaced' => '/rails/active_storage/blobs/redirect/abc123/snap.jpg',
    'and that one prefixed' => '/ru/rails/active_storage/blobs/redirect/abc123/snap.jpg'
  }.each do |what, path|
    test "robots.txt disallows #{what}" do
      assert disallowed?(path), <<~MESSAGE.chomp
        robots.txt allows #{path}

        That is a frame from someone else's camera, published for two days.
        A crawler reading this file is being told it may take it, and the
        crawler that did took 1.5 GB in two hours.
      MESSAGE
    end
  end

  # The other half. Disallowing too much is just as silent, and this site
  # needs to be indexed: Google alone sent 18,166 referrals in fourteen days.
  {
    'the front page' => '/',
    'the Russian front page' => '/ru',
    'getting started' => '/get-started',
    'the hardware browser' => '/supported-hardware/featured',
    'a SoC page' => '/cameras/vendors/sigmastar/socs/ssc338q',
    'a Chinese services page' => '/zh/edge-ai',
    'the sitemap' => '/sitemap.xml'
  }.each do |what, path|
    test "robots.txt still allows #{what}" do
      assert_not disallowed?(path), <<~MESSAGE.chomp
        robots.txt disallows #{path}, which is a page the project wants read.
        Nothing reports this: the crawler simply stops coming and the
        referrals fall off a cliff a month later.
      MESSAGE
    end
  end

  # nginx says the same thing to the crawlers that read robots.txt rarely.
  # The map is the enforcement half; without it a widened robots.txt takes
  # effect whenever the crawler next fetches the file, which for GPTBot was
  # not once in the two hours it spent taking the gallery.
  def gallery_map
    CONF[/map \$http_user_agent \$openipc_gallery_crawler \{(.*?)\n\}/m, 1].to_s
  end

  def refused_agents
    gallery_map.lines.reject { |l| l.strip.start_with?('#') }
               .select { |l| l.strip.end_with?('1;') }
               .map { |l| l.strip.sub(/\s+1;\z/, '') }
               .reject { |p| p.start_with?('default') }
  end

  def refuses?(agent)
    refused_agents.any? { |p| agent.match?(Regexp.new(p.delete_prefix('~*'), Regexp::IGNORECASE)) }
  end

  {
    'GPTBot' => 'Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.4; ' \
                '+https://openai.com/gptbot)',
    'OAI-SearchBot' => 'Mozilla/5.0 (compatible; OAI-SearchBot/1.4; +https://openai.com/searchbot)',
    'ClaudeBot' => 'Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)',
    'PerplexityBot' => 'Mozilla/5.0 (compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)',
    'Bytespider' => 'Mozilla/5.0 (compatible; Bytespider; spider-feedback@bytedance.com)',
    'Amazonbot' => 'Mozilla/5.0 (compatible; Amazonbot/0.1; +https://developer.amazon.com/amazonbot)',
    'Applebot' => 'Mozilla/5.0 (compatible; Applebot/0.1; +http://www.apple.com/go/applebot)'
  }.each do |name, agent|
    test "#{name} is refused the gallery" do
      assert refuses?(agent), "#{name} can still take the Open Wall"
    end
  end

  # The distinction the whole design rests on. These crawlers are welcome on
  # the site; it is the gallery they cannot have. Blocking them at server
  # level would be a different decision, and one nobody has taken.
  test 'the gallery map is applied only in the gallery locations' do
    applied = VHOST.scan(/^(\s*)if \(\$openipc_gallery_crawler\)/).flatten

    assert_equal 3, applied.length, <<~MESSAGE.chomp
      Expected the guard in exactly the three locations that serve gallery
      content -- the hexadecimal snapshot ids, /wall/, and the
      open-wall/snapshots/active_storage catch-all. Found #{applied.length}.
    MESSAGE
    assert applied.all? { |indent| indent.length >= 8 }, <<~MESSAGE.chomp
      The guard appears at server level, which refuses these crawlers the
      whole site. Only the Open Wall is closed to them: they send readers,
      chatgpt.com is already a referrer here, and that trade is a maintainer
      decision recorded in openipc-crawler-block.conf -- not a side effect of
      protecting the gallery.
    MESSAGE
  end

  test 'a reader is never caught by the gallery guard' do
    agent = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) ' \
            'Chrome/145.0.0.0 Safari/537.36'

    assert_not refuses?(agent), 'the gallery is refused to an ordinary browser'
  end

  # Googlebot indexes the pages the project wants found and sends more
  # referrals than everything else combined. It also honours robots.txt, so
  # the gallery is already out of its reach without a 403.
  test 'the search engines are not caught by the gallery guard' do
    ['Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)',
     'Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)',
     'Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)',
     'Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)'].each do |agent|
      assert_not refuses?(agent), "#{agent[/[A-Za-z]+[Bb]ot|Baiduspider/]} is refused the gallery"
    end
  end
end
