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

  # guarded_flash is `transfer && erase && write` -- two gates, not one. The
  # first wording said to run the erase and the write once the transfer had
  # succeeded, which rebuilds only the first gate: a reader whose erase failed
  # would have gone on to write into flash that was never cleared. The rule has
  # to be sequential to cover both, so pin that rather than the sentence.
  test 'the note gates every part on the one before it, not just on the transfer' do
    html = list_of_commands(['tftpboot 0x82000000 f && sf erase 0x0 0x800000 && sf write 0x82000000 0x0 0x1000'])

    assert_includes html, 'only after the part before it has reported success'
    assert_not_includes html, 'after the transfer has reported success'
  end

  # A reader who is entering the parts by hand has to know how many there are.
  # OpenIPC/firmware#2381 lost a camera to counting two: the transfer and the
  # write went in, the erase between them did not, and a NOR write onto
  # unerased flash reports success while storing nothing usable. Nothing in the
  # bootloader's output names the missing step, so the note has to.
  test 'the note counts the parts and says the erase cannot be the one dropped' do
    html = list_of_commands(['tftpboot 0x82000000 f && sf erase 0x0 0x800000 && sf write 0x82000000 0x0 0x1000'])

    assert_includes html, 'the transfer, the erase, then the write'
    assert_includes html, 'Do not leave the erase out'
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

  # OpenIPC/firmware#2405: a stock hi3516ev200 bootloader answered `sf lock 0`
  # with the `sf` usage help, and the reporter stopped there. The subcommand is
  # OpenIPC U-Boot's, so that answer is expected -- but only the page can say
  # so, and it has to say it wherever the line is rendered.
  test 'a block that unlocks the flash is followed by the note' do
    html = list_of_commands(['sf probe 0; sf lock 0;'])

    assert_includes html, 'usage message'
    assert_includes html, 'Nothing was changed by that'
  end

  # Telling the reader to carry on is only half of it. The unlock is there
  # because protected flash swallows an erase and a write and still reports
  # success, so a reader who skipped it has to be able to recognise that later.
  test 'the note says what skipping the unlock costs' do
    html = list_of_commands(['sf probe 0; sf lock 0;'])

    assert_includes html, 'discards an erase and a write while reporting success'
  end

  # Naming the symptom without naming a way out leaves the reader of a genuinely
  # protected chip with a diagnosis and no route to a flashed camera -- the
  # review finding on OpenIPC/website#137. The bootloader cannot clear it, so
  # the way out is something that does not go through the bootloader.
  test 'the note names a way out for a chip that really is protected' do
    html = list_of_commands(['sf probe 0; sf lock 0;'])

    assert_includes html, 'external programmer'
    assert_includes html, 'https://github.com/OpenIPC/defib'
  end

  # Every block that unlocks also chains, so both notes land together. They
  # answer lines in a fixed order -- the unlock comes before the transfer --
  # and the notes have to follow it, or the reader meets the second question
  # first.
  test 'both notes appear, in the order of the lines they answer' do
    html = list_of_commands(['sf probe 0; sf lock 0;',
                             'tftpboot 0x82000000 f && sf erase 0x0 0x800000 && sf write 0x82000000 0x0 0x1000'])

    assert_operator html.index('<code>sf lock 0</code>'), :<,
                    html.index('it does not understand <code>&amp;&amp;</code>')
  end

  # `sf read` and `sf erase` are not it. The backup block probes the flash
  # without unlocking it, and a note about a command it never ran is noise.
  test 'a block that probes the flash without unlocking it is left alone' do
    html = list_of_commands(['sf probe 0; sf read 0x42000000 0x0 0x1000000'])

    assert_equal '<pre class="bg-light p-4">sf probe 0; sf read 0x42000000 0x0 0x1000000</pre>', html
  end
end
