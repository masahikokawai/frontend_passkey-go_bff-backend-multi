require_relative "boot"

require "rails"
require "active_record/railtie"
require "action_controller/railtie"

Bundler.require(*Rails.groups)

module BackendRails
  # CONTRACT.mdセクション20: backendのTask CRUD実処理をRailsで実装したもの
  # (admin/railsとは別コンポーネント)
  #
  # API-onlyモードで、ActiveRecordは既存の tasks/labels/task_labels/users テーブル
  # (backendのgolang-migrateが正本)へ接続するだけで、
  # このアプリ自身はこれらのテーブルを作成するマイグレーションを持たない
  class Application < Rails::Application
    config.load_defaults 7.2
    config.api_only = true

    # このアプリはbffを経由せず直接叩かれる内部API(BFF/gatewayからのみ到達される想定)
    # のため、Railsのセッション/CSRF機構は不要
    config.middleware.delete ActionDispatch::Cookies rescue nil
    config.middleware.delete ActionDispatch::Session::CookieStore rescue nil

    config.autoload_paths << Rails.root.join("app/services")

    # lib/grpc_server・lib/gen(protoc生成コード)はZeitwerkのautoload規約
    # (1ファイル1定数、ファイルパス⇔定数名の対応)に従わない(生成コードが
    # `task/v1/task_pb.rb` から `Task::V1::...` を定義するため)ので、
    # autoload_pathsには入れず$LOAD_PATHへ足すだけにして、明示的なrequireで使う
    # (bin/grpc_server, lib/grpc_server/task_service.rb参照)
    $LOAD_PATH.unshift(Rails.root.join("lib").to_s)
    $LOAD_PATH.unshift(Rails.root.join("lib/gen").to_s)

    config.time_zone = "UTC"
    config.active_record.default_timezone = :utc
  end
end
