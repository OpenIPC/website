# frozen_string_literal: true

require 'test_helper'

# Who is refused at the edge (#234).
#
# This file decides who can reach the site at all, and its two failure modes
# are opposite and both silent. Block too little and the SEO tools keep taking
# bandwidth for nothing. Block too much and a search engine stops indexing us,
# or our own tooling starts getting 403s -- neither of which shows up as an
# error anywhere, because a 403 is a perfectly good response.
#
# The test is not "is it a bot". Googlebot is a bot and is welcome. It is
# whether a crawler has ever sent a reader.
class CrawlerBlockTest < ActiveSupport::TestCase
  CONF = Rails.root.join('deploy/nginx/conf.d/openipc-crawler-block.conf').read.freeze

  # Lines inside the map that set 1, i.e. actually refuse something. The file
  # is mostly commentary about what is deliberately *not* blocked, so reading
  # it without stripping comments reports the opposite of the truth -- which
  # is the mistake page_cache_test made first.
  def blocked_patterns
    map = CONF[/map \$http_user_agent \$openipc_blocked_crawler \{(.*?)\n\}/m, 1].to_s
    map.lines.reject { |l| l.strip.start_with?('#') }
       .select { |l| l.strip.end_with?('1;') }
       .map { |l| l.strip.sub(/\s+1;\z/, '') }
       .reject { |p| p.start_with?('default') }
  end

  def blocks?(agent)
    blocked_patterns.any? do |pattern|
      body = pattern.delete_prefix('"').delete_suffix('"')
      body.start_with?('~*') ? agent.match?(Regexp.new(body.delete_prefix('~*'), Regexp::IGNORECASE))
                             : agent.include?(body)
    end
  end

  test 'it refuses something' do
    refute_empty blocked_patterns, 'the map blocks nobody; this file has stopped doing its job'
  end

  # Each of these sent zero readers across the fourteen days of log the host
  # retains, against a thousand to two thousand requests each.
  {
    'AhrefsBot' => 'Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)',
    'SemrushBot' => 'Mozilla/5.0 (compatible; SemrushBot/7~bl; +http://www.semrush.com/bot.html)',
    'MJ12bot' => 'Mozilla/5.0 (compatible; MJ12bot/v1.4.8; http://mj12bot.com/)',
    'DotBot' => 'Mozilla/5.0 (compatible; DotBot/1.2; +https://opensiteexplorer.org/dotbot)',
    'SofyaBot' => 'Mozilla/5.0 (compatible; SofyaBot/1.0)',
    'Velen' => 'Mozilla/5.0 (compatible; VelenPublicWebCrawler/1.0)',
    'meta-externalagent' => 'meta-externalagent/1.1'
  }.each do |name, agent|
    test "#{name} is refused" do
      assert blocks?(agent), "#{name} sends no readers and is not refused"
    end
  end

  # 2,795 distinct addresses share this one agent in a single day, fetching the
  # ActiveStorage snapshot URLs #146 moved to plain files. A residential-proxy
  # population, so no address rule reaches it.
  test 'the Open Wall scraper is refused' do
    assert blocks?('Mozilla/5.0 (compatible; crawler)')
  end

  # Anchored, so it matches that exact agent and not every string containing
  # the word.
  #
  # The first version of this test listed ClaudeBot, PetalBot and Googlebot as
  # the things that must not be swallowed -- none of which contain "crawler",
  # so replacing the anchored pattern with a bare `~*crawler` left the test
  # green. These agents do contain it.
  test 'the scraper pattern is anchored to that exact agent' do
    ['Mozilla/5.0 (compatible; crawler-for-research/1.0; +https://example.edu/crawler)',
     'Mozilla/5.0 (compatible; ResearchCrawler/2.1; +https://example.org)',
     'SomeCrawler/1.0 (Mozilla/5.0 (compatible; crawler))'].each do |agent|
      assert_not blocks?(agent), <<~MESSAGE.chomp
        The scraper pattern also matches:

          #{agent}

        It is meant to be the exact agent 2,795 addresses share, not every
        string with the word in it -- which would quietly include crawlers
        this file deliberately leaves to a maintainer decision.
      MESSAGE
    end
  end

  # The half that costs readers rather than bandwidth. Google alone sent 18,166
  # referrals in fourteen days.
  {
    'Googlebot' => 'Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)',
    'bingbot' => 'Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)',
    'YandexBot' => 'Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)',
    'Baiduspider' => 'Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)',
    'DuckDuckBot' => 'DuckDuckBot/1.1; (+http://duckduckgo.com/duckduckbot.html)'
  }.each do |name, agent|
    test "#{name} still reaches the site" do
      assert_not blocks?(agent), <<~MESSAGE.chomp
        #{name} is refused. It sends readers, #179's search-console work
        depends on it, and nothing will report the loss -- a 403 is a
        perfectly good response and the traffic simply stops arriving.
      MESSAGE
    end
  end

  # Our own Playwright checks and screenshot runs come from the build host as
  # HeadlessChrome. #234 lists "a headless Chrome at 2,568" among the crawlers
  # worth naming; it is us, and blocking it would break tools/shot.mjs,
  # tools/whatnext-check.mjs and every check that follows them.
  test 'our own headless browser is not refused' do
    agent = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) ' \
            'HeadlessChrome/131.0.6778.33 Safari/537.36'

    assert_not blocks?(agent), <<~MESSAGE.chomp
      HeadlessChrome is refused, which is this project's own build host
      running the checks in tools/. They load real pages exactly as a reader's
      browser would, which is the whole point of them.
    MESSAGE
  end

  # Named in the file rather than decided by it, per #234 itself. If one of
  # them is later blocked that should be a deliberate edit with a reason, not
  # a side effect of widening a pattern.
  test 'the crawlers left to a maintainer decision are still documented' do
    %w[Anthropic OpenAI Perplexity YisouSpider Sogou Bytespider PetalBot].each do |name|
      assert_includes CONF, name, <<~MESSAGE.chomp
        #{name} is no longer mentioned. It was left unblocked as a decision
        for the maintainers -- the LLM crawlers send some traffic back, and
        the Chinese ones serve the largest locale on this site. Dropping the
        note loses the reasoning and the next person re-derives it.
      MESSAGE
    end
  end
end
