class GithubController < ApplicationController
  skip_before_action :verify_authenticity_token

  def create
    permitted_params = params.permit(:platform, :release, :toolchain)
    Soc.where(family: permitted_params[:platform]).each do |s|
      s.update toolchain: permitted_params[:toolchain]
    end
    head :ok
  end
end
