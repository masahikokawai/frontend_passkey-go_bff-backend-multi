Rails.application.config.middleware.insert_before 0, Rack::Cors do
  allow do
    origins AppConfig.frontend_origin
    resource "/api/*",
      headers: :any,
      methods: %i[get post],
      credentials: true
  end
end
