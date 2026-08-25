# frozen_string_literal: true

require 'test_helper'

class InstallationHelperTest < ActionView::TestCase
  include InstallationHelper

  # The note belongs to the blocks that chain with `&&` and to no others. A
  # page carries several blocks and only some of them chain, so this is the
  # level where the rule is visible; the controller test covers it end to end.
  test 'a block that chains with && is followed by the note' do
    html = list_of_commands(['tftpboot 0x82000000 f && sf erase 0x0 0x800000'])

    assert_includes html, 'it does not understand <code>&amp;&amp;</code>'
  end

  # `run setnor8m`, `run uknand; run urnand`, the mw.b lines -- none of these
  # chain, so the note would be answering a question the block has not raised.
  test 'a block that does not chain is left alone' do
    html = list_of_commands(['run setnor8m'])

    assert_equal '<pre class="bg-light p-4">run setnor8m</pre>', html
  end

  test 'the note is html, not escaped markup' do
    html = list_of_commands(['tftp 0x82000000 f && sf erase 0x0 0x800000'])

    assert_includes html, '<code>&amp;&amp;</code>'
    assert_not_includes html, '&lt;code&gt;'
  end
end
