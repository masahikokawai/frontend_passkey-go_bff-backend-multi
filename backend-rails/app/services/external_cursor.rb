# 外部公開API v2(keysetページング)の cursor 符号化(backend/internal/service/task_external.go の
# encodeExternalCursor/decodeExternalCursor と同一形式)
# 中身は "<RFC3339Nano>|<id>" をbase64url化しただけの不透明文字列(opaque cursor)
class ExternalCursor
  class InvalidCursor < StandardError; end

  Position = Struct.new(:created_at, :id, keyword_init: true)

  def self.encode(created_at, id)
    # RubyのTime#iso8601(9)はGoのtime.RFC3339Nano相当(ナノ秒までの小数点付きRFC3339)を再現する
    raw = "#{created_at.utc.iso8601(9)}|#{id}"
    Base64.urlsafe_encode64(raw)
  end

  def self.decode(cursor)
    raw = Base64.urlsafe_decode64(cursor)
    created_at_str, id_str = raw.split("|", 2)
    raise InvalidCursor, "cursorの区切りが不正です" if id_str.nil?

    created_at = Time.iso8601(created_at_str)
    id = Integer(id_str, exception: false)
    raise InvalidCursor, "idの形式が不正です" if id.nil?

    Position.new(created_at: created_at, id: id)
  rescue ArgumentError => e
    raise InvalidCursor, "cursorのデコードに失敗しました: #{e.message}"
  end
end
