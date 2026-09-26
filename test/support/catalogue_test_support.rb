# frozen_string_literal: true

# Each test starts from an empty catalogue, the way each started from empty
# socs and vendors tables while the catalogue was a database (#289). A test
# builds the chips it needs with `create!`, which registers them in that
# test's catalogue and nowhere else; one that wants the real files asks for
# `Catalogue.load`.
#
# `create!` exists only here. The application has no way to add to the
# catalogue at runtime, and should not: it changes in a pull request.
module CatalogueTestSupport
  extend ActiveSupport::Concern

  included do
    setup { Catalogue.current = Catalogue.new }
    teardown { Catalogue.current = nil }
  end
end

class Vendor
  def self.create!(**attributes)
    Catalogue.current.add_vendor(new(**attributes))
  end
end

class Soc
  def self.create!(**attributes)
    Catalogue.current.add_soc(new(**attributes))
  end
end
