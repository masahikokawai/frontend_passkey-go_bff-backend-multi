require_relative "boot"

require "rails"
# Pick the frameworks you want:
require "active_model/railtie"
require "active_job/railtie"
# require "active_record/railtie"
# require "active_storage/engine"
require "action_controller/railtie"
require "action_mailer/railtie"
# require "action_mailbox/engine"
# require "action_text/engine"
require "action_view/railtie"
require "action_cable/engine"
# require "rails/test_unit/railtie"

# 【実装時に発見した実際の非互換性】omniauth-openid-connect(jjbohn版、2020年以降更新停止)が
# 固定依存するopenid_connect 0.9.2は、Rails 4時代のActiveSupportにあった
# `Module#alias_method_chain`(Rails 5で削除済み)を使っており、これが無いと
# `NoMethodError: undefined method 'alias_method_chain'`でgemのrequire自体が失敗する。
# 「omniauth-openid-connect gemを実際に使う」という比較の趣旨を優先し、削除されたメソッドを
# 最小限のshimとして復元する(古いgemを現行Railsで動かす際によく使われる、実務的な回避策)
class Module
  def alias_method_chain(target, feature)
    alias_method "#{target}_without_#{feature}", target
    alias_method target, "#{target}_with_#{feature}"
  end unless method_defined?(:alias_method_chain)
end

# Require the gems listed in Gemfile, including any gems
# you've limited to :test, :development, or :production.
Bundler.require(*Rails.groups)

module BffRails
  class Application < Rails::Application
    # Initialize configuration defaults for originally generated Rails version.
    config.load_defaults 8.0

    # Please, add to the `ignore` list any other `lib` subdirectories that do
    # not contain `.rb` files, or that should not be reloaded or eager loaded.
    # Common ones are `templates`, `generators`, or `middleware`, for example.
    config.autoload_lib(ignore: %w[assets tasks])

    # Configuration for the application, engines, and railties goes here.
    #
    # These settings can be overridden in specific environments using the files
    # in config/environments, which are processed later.
    #
    # config.time_zone = "Central Time (US & Canada)"
    # config.eager_load_paths << Rails.root.join("extras")

    # Only loads a smaller set of middleware suitable for API only apps.
    # Middleware like session, flash, cookies can be added back manually.
    # Skip views, helpers and assets when generating a new resource.
    config.api_only = true
  end
end
