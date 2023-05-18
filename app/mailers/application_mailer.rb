# frozen_string_literal: true

class ApplicationMailer < ActionMailer::Base
  default from: 'robot@openipc.org'
  layout 'mailer'

  def experror
    @err = params[:error]
    mail to: 'paul@openipc.org', subject: "OpenIPC.org in #{Rails.env}: #{@err.message}"
  end

  def missing_page
    @payload = params[:request]
    mail to: 'paul@openipc.org',
         cc: 'igor@openipc.org',
         subject: 'OpenIPC.org is missing a page'
  end
end
