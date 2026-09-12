require "rails_helper"

# 簡易的なe2e
# 実ブラウザ(Selenium)は使わずrack_testドライバで、
# 「一覧→編集→保存→一覧で反映確認→変更履歴に記録されている」という
# 画面遷移込みの一連の導線をブラウザ操作に近い形で確認する
RSpec.describe "FeatureFlags", type: :system do
  let!(:flag) do
    FactoryBot.create(:feature_flag, flag_key: "bff.tasks-backend-v2", default_variation: "off", enabled: true)
  end

  around do |example|
    original = ENV["FORCE_BASIC_AUTH_IN_TEST"]
    ENV["FORCE_BASIC_AUTH_IN_TEST"] = "1"
    example.run
  ensure
    ENV["FORCE_BASIC_AUTH_IN_TEST"] = original
  end

  it "一覧→編集→保存→一覧反映→変更履歴確認、の一連の導線が通る" do
    # Capybara 3.40のrack_testドライバには`basic_authenticate`メソッドが無くなっている
    # (バージョンによってAPIが変わりやすい箇所)
    # より安定している `driver.header` で Authorization ヘッダを直接組み立てる方法を使う
    user = ENV.fetch("ADMIN_BASIC_AUTH_USER", "admin")
    password = ENV.fetch("ADMIN_BASIC_AUTH_PASSWORD", "password")
    credentials = Base64.strict_encode64("#{user}:#{password}")
    page.driver.header("Authorization", "Basic #{credentials}")

    # 1. 一覧画面にflag_keyが表示されている
    visit root_path
    expect(page).to have_content("bff.tasks-backend-v2")
    expect(page).to have_content("off")

    # 2. 編集画面へ遷移し、default_variationをonへ切り替えて送信
    click_link "編集"
    expect(page).to have_current_path(edit_feature_flag_path(flag))
    select "on", from: "feature_flag_default_variation"
    click_button "保存"

    # 3. 一覧画面に戻り、変更後の値が反映されている
    expect(page).to have_current_path(root_path)
    expect(page).to have_content("bff.tasks-backend-v2")
    expect(flag.reload.default_variation).to eq("on")

    # 4. 変更履歴に今回の変更が1行増えている
    visit audit_logs_feature_flag_path(flag)
    expect(page).to have_content("off").and have_content("on")
    expect(flag.feature_flag_audit_logs.count).to eq(1)
  end
end
