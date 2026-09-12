FactoryBot.define do
  factory :feature_flag do
    sequence(:flag_key) { |n| "test.flag-#{n}" }
    description { "テスト用フラグ" }
    default_variation { "off" }
    enabled { true }
  end
end
