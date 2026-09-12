require "rails_helper"

RSpec.describe "FeatureFlags", type: :request do
  let!(:flag) { FactoryBot.create(:feature_flag, flag_key: "frontend.tasks-ts-rewrite", default_variation: "off", enabled: true) }

  describe "GET /" do
    it "一覧が表示される" do
      get root_path
      expect(response).to have_http_status(:ok)
      expect(response.body).to include("frontend.tasks-ts-rewrite")
    end
  end

  describe "GET /feature_flags/:id/edit" do
    it "編集フォームが表示される" do
      get edit_feature_flag_path(flag)
      expect(response).to have_http_status(:ok)
    end

    it "存在しないIDだと一覧へリダイレクトされエラーメッセージが表示される(admin/goのEdit_NotFoundと同じ観点)" do
      # 【実機検証で発覚した不具合の回帰テスト】以前はRailsの生の例外ページがそのまま表示され、一覧へ戻る手段が無かった
      # ApplicationController#render_record_not_found
      # (rescue_from ActiveRecord::RecordNotFound)により、常にナビゲーション付きの
      # 一覧画面へリダイレクトされるようにした
      get edit_feature_flag_path(id: 999_999_999)
      expect(response).to redirect_to(root_path)
      follow_redirect!
      expect(response.body).to include("指定されたデータが見つかりません")
    end
  end

  describe "PATCH /feature_flags/:id" do
    it "更新に成功し、feature_flag_audit_logsへ1行追記される" do
      expect {
        patch feature_flag_path(flag), params: { feature_flag: { default_variation: "on", enabled: "1" } }
      }.to change { flag.reload.default_variation }.from("off").to("on")
        .and change { FeatureFlagAuditLog.count }.by(1)

      expect(response).to redirect_to(root_path)

      audit_log = FeatureFlagAuditLog.order(:id).last
      expect(audit_log.flag_key).to eq("frontend.tasks-ts-rewrite")
      expect(audit_log.before_default_variation).to eq("off")
      expect(audit_log.after_default_variation).to eq("on")
      expect(audit_log.changed_by).to be_present
    end

    it "不正な値(on/off以外)だと更新に失敗し、監査ログも作られない" do
      expect {
        patch feature_flag_path(flag), params: { feature_flag: { default_variation: "maybe" } }
      }.not_to change { FeatureFlagAuditLog.count }
      expect(response).to have_http_status(:unprocessable_content)
    end

    it "存在しないIDだと一覧へリダイレクトされエラーメッセージが表示される" do
      patch feature_flag_path(id: 999_999_999), params: { feature_flag: { default_variation: "on" } }
      expect(response).to redirect_to(root_path)
    end

    # CONTRACT.mdセクション19: 多値フラグ(admin/goのTestFeatureFlagService_Update_AcceptsMultivariateFlagと対になる観点)
    context "多値フラグ(inline/modal/page)の場合" do
      let!(:multivariate_flag) do
        FactoryBot.create(:feature_flag,
          flag_key: "frontend.task-create-ux",
          default_variation: "inline",
          variations: { "inline" => "inline", "modal" => "modal", "page" => "page" },
          enabled: true)
      end

      it "on/off以外の値(variationsに定義されたキー)で更新できる" do
        expect {
          patch feature_flag_path(multivariate_flag), params: { feature_flag: { default_variation: "modal", enabled: "1" } }
        }.to change { multivariate_flag.reload.default_variation }.from("inline").to("modal")
        expect(response).to redirect_to(root_path)
      end

      it "variationsに存在しない値だと更新に失敗する" do
        patch feature_flag_path(multivariate_flag), params: { feature_flag: { default_variation: "not-a-real-variation" } }
        expect(response).to have_http_status(:unprocessable_content)
        expect(multivariate_flag.reload.default_variation).to eq("inline")
      end

      it "編集画面に3つの選択肢がすべて表示される(ON/OFFトグルではなく汎用select)" do
        get edit_feature_flag_path(multivariate_flag)
        expect(response.body).to include('value="inline"')
        expect(response.body).to include('value="modal"')
        expect(response.body).to include('value="page"')
      end
    end
  end

  describe "GET /feature_flags/:id/audit_logs" do
    it "変更履歴が表示される" do
      patch feature_flag_path(flag), params: { feature_flag: { default_variation: "on", enabled: "1" } }
      get audit_logs_feature_flag_path(flag)
      expect(response).to have_http_status(:ok)
      expect(response.body).to include("off").and include("on")
    end

    it "存在しないIDだと一覧へリダイレクトされエラーメッセージが表示される" do
      get audit_logs_feature_flag_path(id: 999_999_999)
      expect(response).to redirect_to(root_path)
    end

    it "複数回更新すると、その回数分だけ audit_log が記録される(admin/goのOrdersNewestFirstと対になる観点。表示順自体はfeature_flag_spec.rbのモデルレベルで検証)" do
      patch feature_flag_path(flag), params: { feature_flag: { default_variation: "on", enabled: "1" } }
      patch feature_flag_path(flag), params: { feature_flag: { default_variation: "off", enabled: "1" } }
      patch feature_flag_path(flag), params: { feature_flag: { default_variation: "on", enabled: "1" } }

      get audit_logs_feature_flag_path(flag)

      expect(response).to have_http_status(:ok)
      expect(flag.feature_flag_audit_logs.count).to eq(3)
    end
  end

  describe "Basic Auth" do
    around do |example|
      original = ENV["FORCE_BASIC_AUTH_IN_TEST"]
      ENV["FORCE_BASIC_AUTH_IN_TEST"] = "1"
      example.run
    ensure
      ENV["FORCE_BASIC_AUTH_IN_TEST"] = original
    end

    it "資格情報が無いと401になる" do
      get root_path
      expect(response).to have_http_status(:unauthorized)
    end

    it "正しい資格情報なら200になる" do
      user = ENV.fetch("ADMIN_BASIC_AUTH_USER", "admin")
      password = ENV.fetch("ADMIN_BASIC_AUTH_PASSWORD", "password")
      get root_path, headers: { "HTTP_AUTHORIZATION" => ActionController::HttpAuthentication::Basic.encode_credentials(user, password) }
      expect(response).to have_http_status(:ok)
    end

    it "パスワードが誤っていると401になる(admin/goと同じ観点)" do
      user = ENV.fetch("ADMIN_BASIC_AUTH_USER", "admin")
      get root_path, headers: { "HTTP_AUTHORIZATION" => ActionController::HttpAuthentication::Basic.encode_credentials(user, "wrong-password") }
      expect(response).to have_http_status(:unauthorized)
    end
  end
end
