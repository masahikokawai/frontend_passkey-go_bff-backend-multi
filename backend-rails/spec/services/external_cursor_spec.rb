require "rails_helper"

# backend/internal/service/task_external.go の encodeExternalCursor/decodeExternalCursor 相当
RSpec.describe ExternalCursor do
  it "encode→decodeで元の(created_at, id)に往復する" do
    created_at = Time.utc(2026, 9, 8, 12, 0, 0)
    cursor = described_class.encode(created_at, 42)

    pos = described_class.decode(cursor)

    expect(pos.created_at).to eq(created_at)
    expect(pos.id).to eq(42)
  end

  it "base64として不正な文字列はInvalidCursorになる" do
    expect { described_class.decode("!!!not-base64!!!") }.to raise_error(ExternalCursor::InvalidCursor)
  end

  it "区切り(|)が無い文字列はInvalidCursorになる" do
    cursor = Base64.urlsafe_encode64("no-separator-here")
    expect { described_class.decode(cursor) }.to raise_error(ExternalCursor::InvalidCursor)
  end

  it "idの部分が数値でなければInvalidCursorになる" do
    cursor = Base64.urlsafe_encode64("2026-09-08T12:00:00Z|not-a-number")
    expect { described_class.decode(cursor) }.to raise_error(ExternalCursor::InvalidCursor)
  end
end
