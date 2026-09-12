# CONTRACT.mdセクション11・20.9: 外部公開API(BFF非経由、Client Credentials Grant)専用の
# Pumaプロセス
# 内部REST(config/puma.rb、既定:8096)とは別プロセス・別ポートで起動する
# (bin/grpc_serverと合わせるとRailsは合計3プロセス構成になるREADME参照)
# 既定:8101
port ENV.fetch("EXTERNAL_HTTP_ADDR", ":8101").to_s.delete_prefix(":")

threads_count = ENV.fetch("RAILS_MAX_THREADS") { 5 }
threads threads_count, threads_count

environment ENV.fetch("RAILS_ENV") { "development" }

plugin :tmp_restart
