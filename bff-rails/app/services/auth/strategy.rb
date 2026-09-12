module Auth
  module Strategy
    def self.for(gem_name)
      case gem_name
      when "openid_connect"
        OpenidConnectGemLogin.new
      else
        OmniauthGemLogin.new
      end
    end
  end
end
