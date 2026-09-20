# frozen_string_literal: true

require 'test_helper'

# The allocator settings are four lines in a Dockerfile that nothing else in
# the suite looks at, and each of them was arrived at by measurement that
# contradicted the obvious choice (#148). A later edit that drops one would
# change how much memory production uses and break no test at all -- so these
# are the tests.
class AllocatorTest < ActiveSupport::TestCase
  DOCKERFILE = Rails.root.join('Dockerfile').read.freeze

  # The instructions, without the prose. The Dockerfile explains at length why
  # MALLOC_ARENA_MAX is not set, and a test looking for the string alone would
  # be tripped by its own explanation.
  DIRECTIVES = DOCKERFILE.lines.reject { |l| l.strip.start_with?('#') }.join.freeze

  test 'the runtime image installs jemalloc' do
    assert_match(/^\s+libjemalloc2\s*\\?$/, DIRECTIVES,
                 'libjemalloc2 is what LD_PRELOAD below resolves to')
  end

  test 'jemalloc is preloaded by bare soname' do
    assert_match(/LD_PRELOAD=libjemalloc\.so\.2/, DIRECTIVES,
                 'a bare soname lets the loader find it under whichever multiarch ' \
                 'directory this architecture uses; a hard path would pin it to amd64')
  end

  # The measurement, in this image, 32 threads churning 5MB strings, RSS after
  # the work and twenty seconds later: glibc 18.7 -> 18.5 MB; jemalloc with
  # default settings 360 -> 345 MB; jemalloc with these settings 316 -> 21 MB.
  # Left alone, jemalloc purges a dirty page only when something else allocates
  # in the same arena, so a quiet worker keeps its peak forever.
  test 'jemalloc is told to give the pages back' do
    assert_match(/MALLOC_CONF=\S*background_thread:true/, DIRECTIVES,
                 'without a background thread the decay has no clock of its own, ' \
                 'and jemalloc becomes a memory regression rather than a saving')
    assert_match(/MALLOC_CONF=\S*dirty_decay_ms:\d+/, DIRECTIVES)
    assert_match(/MALLOC_CONF=\S*muzzy_decay_ms:\d+/, DIRECTIVES)
  end

  # Measured harmful, not merely redundant: on many small allocations across 32
  # threads, glibc capped at two arenas retained 123MB where the default
  # retained 72MB. It is also inert whenever the preload works, so reinstating
  # it would look free and would not be.
  test 'the glibc arena cap is not reinstated' do
    assert_no_match(/MALLOC_ARENA_MAX/, DIRECTIVES,
                    'see the comment above LD_PRELOAD in the Dockerfile')
  end

  # LD_PRELOAD to a missing library is a no-op: the process keeps running on
  # glibc and the only symptom is memory drifting back, months later, with
  # nothing to point at. The build has to be what notices.
  test 'the build refuses to produce an image where the preload does not take' do
    assert_match(/RUN LD_PRELOAD=libjemalloc\.so\.2 ruby -e/, DIRECTIVES)
    assert_match(%r{abort .*unless File\.read\("/proc/self/maps"\)\.include\?\("jemalloc"\)},
                 DIRECTIVES)
  end

  # As an ENV it applies to every later RUN too, so setting it above the
  # apt-get line makes the loader complain on every command in the build.
  test 'the preload is set after the install that provides it' do
    install = DIRECTIVES.index('libjemalloc2')
    preload = DIRECTIVES.index('ENV LD_PRELOAD=')

    assert install, 'libjemalloc2 is not installed at all'
    assert preload, 'LD_PRELOAD is not set at all'
    assert_operator install, :<, preload,
                    'ENV LD_PRELOAD must come after the apt-get that installs the library'
  end
end
