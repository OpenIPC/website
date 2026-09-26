# frozen_string_literal: true

# Read data/catalogue at boot rather than on the first request that needs it
# (#289). A file that does not load -- an unsafe slug, a duplicate, a status
# nobody draws -- then stops the container before its health check passes, so
# openipc-deploy refuses it and the running release keeps serving. Loaded
# lazily, the same file would have answered 500 on every catalogue address.
#
# Not in tests, which start each case from a catalogue of their own.
Rails.application.config.after_initialize do
  Catalogue.current unless Rails.env.test?
end
