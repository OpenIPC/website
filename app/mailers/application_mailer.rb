class ApplicationMailer < ActionMailer::Base
  default from: "robot@openipc.org"
  layout "mailer"

  def experror
    @err = params[:error]
    mail to: "paul@openipc.org", subject: "OpenIPC.org in #{Rails.env}: #{@err.message}"
  end
end
