# Where the I18n library should search for translation files
I18n.load_path += Dir[Rails.root.join('lib', 'locale', '*.{rb,yml}')]

# Permitted locales available for the application.
#
# Three, not ten. The site carried ten locale files and served none of them,
# because Multilang's set_locale was switched off deliberately -- the project
# did not have the people to keep ten translations in step with the English
# copy, and a stale translation of the flashing instructions is worse than an
# English one. Translations are cheaper to produce now; keeping them honest
# through every later edit is not, so the list is what the project believes it
# can maintain: English as the source, Russian and Chinese for the two largest
# communities.
I18n.available_locales = %i[en ru zh]

# Set default locale to something other than :en
I18n.default_locale = :en
