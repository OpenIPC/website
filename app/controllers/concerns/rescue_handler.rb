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

      ApplicationMailer.with(error: exception).experror.deliver
      render file: Rails.public_path.join('500.html'), layout: false, status: 500
    end
  end
end
