# frozen_string_literal: true

# CLDR plural rules for the locales that need more than one/other.
#
# I18n's default pluralizer knows two forms, so Russian counts from 2 upwards
# all took the `other` string: "5 ошибки" and "21 ошибки" instead of "5 ошибок"
# and "21 ошибка". Adding `few` and `many` to the locale file alone does not fix
# that -- nothing would ever select them -- so the rule has to come with them.
#
# rails-i18n carries these for every locale and would be the answer if the site
# ever serves more than three. For three, one of which needs no rule at all, a
# gem is more than the problem is worth.
#
# config/initializers/locale.rb adds lib/locale/*.rb to I18n.load_path and mixes
# I18n::Backend::Pluralization into the backend; without that include this file
# is read and ignored.
#
# Chinese has a single form, which is what the default pluralizer already does,
# so it needs no entry here.
{
  ru: {
    i18n: {
      plural: {
        keys: %i[one few many other],
        rule: lambda { |n|
          # `other` is CLDR's form for fractions; integers take the three below.
          next :other unless n.is_a?(Integer)

          mod10 = n % 10
          mod100 = n % 100

          next :one if mod10 == 1 && mod100 != 11
          next :few if (2..4).cover?(mod10) && !(12..14).cover?(mod100)

          :many
        }
      }
    }
  }
}
