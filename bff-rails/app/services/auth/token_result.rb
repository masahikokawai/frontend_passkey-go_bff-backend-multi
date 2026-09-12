module Auth
  TokenResult = Struct.new(:access_token, :refresh_token, :sub, :name, :email, keyword_init: true)
end
