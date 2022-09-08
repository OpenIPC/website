class GithubController < ApplicationController
  skip_before_action :verify_authenticity_token

  def create
    permitted_params = params.permit(:platform, :release, :token, :toolchain)
    family = permitted_params[:platform].upcase
    toolchain_filename = permitted_params[:toolchain]
    token = permitted_params[:token]
    if token.eql?(Rails.application.credentials.dig(:github, :token))
      Soc.where(family: family).each do |s|
        s.update toolchain_filename: toolchain_filename
      end
      head :ok
    else
      head 403
    end
  end
end
