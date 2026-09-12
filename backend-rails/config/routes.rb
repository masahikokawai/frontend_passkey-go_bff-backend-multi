Rails.application.routes.draw do
  namespace :internal do
    namespace :v1 do
      resources :tasks, only: %i[index show create update destroy]
    end
  end

  # CONTRACT.mdセクション11: BFF非経由の外部公開API
  # 既定では内部用puma(:8096)にも同じルートが乗るが、
  # 実際に到達させるのは config/puma_external.rb(既定:8101)で
  # 起動した別プロセス経由のみとする運用(READMEの起動手順を参照)
  namespace :external do
    namespace :v1 do
      resources :tasks, only: %i[index]
    end
  end
end
