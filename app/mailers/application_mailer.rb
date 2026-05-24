# frozen_string_literal: true

class ApplicationMailer < ActionMailer::Base
  default from: 'dev@openipc.org'
  layout 'mailer'

  def experror
    @err = params[:error]
    mail to: 'flyrouter@openipc.org', subject: "OpenIPC.org in #{Rails.env}: #{@err.message}"
  end

  def missing_page
    @payload = params[:request]
    mail to: 'flyrouter@openipc.org',
         cc: 'vixand@openipc.org',
         subject: 'OpenIPC.org is missing a page'
  end
end
