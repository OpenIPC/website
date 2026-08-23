# frozen_string_literal: true

module RescueHandler
  extend ActiveSupport::Concern

  included do
    rescue_from StandardError, with: :rescue_ladder if Rails.env.production?
  end

  private

  def rescue_ladder(exception)
    case exception
    when ActiveRecord::RecordNotFound
      redirect_to root_path, alert: 'Record not found.'
    when ActionView::MissingTemplate
      render file: Rails.public_path.join('404.html'), layout: false, status: :not_found
    when ActionController::UnknownFormat
      render file: Rails.public_path.join('404.html'), layout: false, status: :not_found
      # render plain: 'Wrong request', status: 404
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

    ApplicationMailer.with(error: exception).experror.deliver
  rescue StandardError => e
    Rails.logger.error("[rescue_ladder] could not send error mail: #{e.class}: #{e.message}")
  end
end
