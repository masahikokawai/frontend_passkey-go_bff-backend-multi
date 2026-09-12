# CONTRACT.mdセクション20: REST(ActionController)の待受ポート。既定:8096
# (Go実装:8090/Rust実装:8093/Scala実装群:8094-8095と衝突しない値)
port ENV.fetch("HTTP_ADDR", ":8096").to_s.delete_prefix(":")

threads_count = ENV.fetch("RAILS_MAX_THREADS") { 5 }
threads threads_count, threads_count

environment ENV.fetch("RAILS_ENV") { "development" }

plugin :tmp_restart
