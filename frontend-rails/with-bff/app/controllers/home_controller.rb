class HomeController < ApplicationController
  def index
    @bff_rails_origin = ENV.fetch("BFF_RAILS_ORIGIN", "http://localhost:8102")
  end
end
