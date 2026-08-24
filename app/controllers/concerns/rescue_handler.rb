# frozen_string_literal: true

module RescueHandler
  extend ActiveSupport::Concern

  included do
    rescue_from StandardError, with: :rescue_ladder if Rails.env.production?
  end

  private

  def rescue_ladder(exception)
    case exception
    # A URL naming a record that is not there, a template that is not there,
    # a format nothing can render: one answer for all three.
    #
    # RecordNotFound used to redirect to the homepage with a flash instead,
    # which is kind to someone who mistyped and a lie to everything else -- a
    # crawler, a stale link and a monitor all saw 302-then-200 for a dead SoC
    # page. The other two already answered 404.
    when ActiveRecord::RecordNotFound, ActionView::MissingTemplate,
         ActionController::UnknownFormat
      render file: Rails.public_path.join('404.html'), layout: false, status: :not_found
    when ActionController::InvalidAuthenticityToken
      redirect_to root_path, alert: 'Session expired. Please sign in..'
    else
      raise exception unless Rails.env.production?

      notify_of(exception)
      render file: Rails.public_path.join('500.html'), layout: false, status: 500
    end
  end

  # Notifying about a failure must never itself become a failure. Without this,
  # any delivery problem (no MTA, unroutable relay) turns a handled exception
  # into an unhandled one and the 500.html below is never rendered.
  #
  # Always log first. Mailing the error but never logging it means a broken
  # page is invisible in the logs, which is how the missing "stage-*" asset
  # went unnoticed until the container parity check.
  def notify_of(exception)
    Rails.logger.error("[rescue_ladder] #{exception.class}: #{exception.message}")
    Rails.logger.error(exception.backtrace&.first(20)&.join("\n")) if exception.backtrace

    return unless error_mail_enabled?

    ApplicationMailer.with(error: exception).experror.deliver
  rescue StandardError => e
    Rails.logger.error("[rescue_ladder] could not send error mail: #{e.class}: #{e.message}")
  end

  # Off by default. One email per unhandled exception is unusable on a public
  # site: crawlers hit the app continuously, so a transient fault -- a database
  # restart during a package upgrade, say -- turns into a mail flood. This was
  # invisible for years only because delivery was broken; the moment routing was
  # fixed it produced dozens of messages a minute.
  #
  # Every exception is logged above regardless, which is the durable record.
  # Set ERROR_MAIL=1 to opt back in.
  def error_mail_enabled?
    ENV['ERROR_MAIL'].present?
  end
end
