require "rails_helper"

# CONTRACT.mdセクション5.1のJSON契約を、REST経由で実際にend-to-endに確認する。
# backend(Go)側のhandler/v1/task_test.go相当。
RSpec.describe "Internal::V1::Tasks", type: :request do
  let(:hmac_secret) { "test-hmac-secret" }
  let!(:user) { User.create!(keycloak_sub: "sub-1", email: "u@example.com", name: "U", role: 1) }
  let!(:other_user) { User.create!(keycloak_sub: "sub-2", email: "other@example.com", name: "Other", role: 1) }

  before do
    allow(JwtVerifier).to receive(:new).and_return(
      JwtVerifier.new(local_hmac_secret: hmac_secret, expected_audience: "backend")
    )
  end

  def bearer_for(u)
    token = JWT.encode(
      { sub: u.id.to_s, iss: JwtVerifier::LOCAL_HMAC_ISSUER, aud: "backend", exp: 1.hour.from_now.to_i },
      hmac_secret, "HS256"
    )
    { "Authorization" => "Bearer #{token}" }
  end

  describe "GET /internal/v1/tasks" do
    it "Authorizationヘッダが無ければ401 unauthorized" do
      get "/internal/v1/tasks"
      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)).to eq("error" => "unauthorized")
    end

    it "トークンの検証に失敗すれば401 invalid_token" do
      get "/internal/v1/tasks", headers: { "Authorization" => "Bearer garbage" }
      expect(response).to have_http_status(:unauthorized)
      expect(JSON.parse(response.body)).to eq("error" => "invalid_token")
    end

    it "有効なトークンなら自分のtaskだけを返す(他人のtaskは含まれない)" do
      user.tasks.create!(name: "mine", status: "waiting", finished_on: Date.today)
      other_user.tasks.create!(name: "not-mine", status: "waiting", finished_on: Date.today)

      get "/internal/v1/tasks", headers: bearer_for(user)

      expect(response).to have_http_status(:ok)
      body = JSON.parse(response.body)
      expect(body["tasks"].map { |t| t["name"] }).to eq(["mine"])
      expect(body["total"]).to eq(1)
    end

    it "status不正なら422 invalid_status" do
      get "/internal/v1/tasks", params: { status: "bogus" }, headers: bearer_for(user)
      expect(response).to have_http_status(:unprocessable_entity)
      expect(JSON.parse(response.body)).to eq("error" => "invalid_status")
    end
  end

  describe "POST /internal/v1/tasks" do
    it "正常な入力なら201でCONTRACT.mdセクション5.1の形状を返す" do
      post "/internal/v1/tasks", params: { name: "t1", status: "waiting", finished_on: "2030-01-01" },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:created)
      body = JSON.parse(response.body)
      expect(body).to include("name" => "t1", "status" => "waiting", "finished_on" => "2030-01-01", "user_id" => user.id)
      expect(body["created_at"]).to match(/\A\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\z/)
    end

    it "nameが無ければ400 invalid_request" do
      post "/internal/v1/tasks", params: { status: "waiting", finished_on: "2030-01-01" }, headers: bearer_for(user)
      expect(response).to have_http_status(:bad_request)
      expect(JSON.parse(response.body)).to eq("error" => "invalid_request")
    end

    it "statusが不正なら422 invalid_status" do
      post "/internal/v1/tasks", params: { name: "t1", status: "bogus", finished_on: "2030-01-01" },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:unprocessable_entity)
      expect(JSON.parse(response.body)).to eq("error" => "invalid_status")
    end

    it "finished_onが不正な日付形式なら422 invalid_finished_on" do
      post "/internal/v1/tasks", params: { name: "t1", status: "waiting", finished_on: "not-a-date" },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:unprocessable_entity)
      expect(JSON.parse(response.body)).to eq("error" => "invalid_finished_on")
    end

    it "nameが21文字以上なら422 validation_error" do
      post "/internal/v1/tasks", params: { name: "a" * 21, status: "waiting", finished_on: "2030-01-01" },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:unprocessable_entity)
      expect(JSON.parse(response.body)["error"]).to eq("validation_error")
    end

    # 【テスト監査で追加】NULバイト(\x00)・他のC0制御文字(\x01等)を含むname/descriptionが、
    # mysql2アダプタ経由で切り詰められず(strlen的なC文字列境界バグの懸念)そのままDBへ
    # 往復することを確認する(backend(Go)側でも同種のテストを追加、GORM/bob両方で確認済み)
    it "NULバイトや制御文字を含むname/descriptionでも切り詰められずそのまま保存・返却される" do
      name = "a\x00b"
      description = "x\x00y\x01z"
      post "/internal/v1/tasks", params: { name: name, description: description, status: "waiting", finished_on: "2030-01-01" },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:created)
      body = JSON.parse(response.body)
      expect(body["name"]).to eq(name)
      expect(body["description"]).to eq(description)

      created = TaskRecord.find(body["id"])
      expect(created.name).to eq(name)
      expect(created.description).to eq(description)
    end

    # 【テストカバレッジ監査で発覚した実際のバグ】Go/Rust/Scala(http4s/Pekko)は全て
    # finished_onが過去日なら422 validation_errorで拒否するが、Rails版はこのテストが
    # 無く、実装(app/models/task_record.rb)にも過去日チェックが存在しなかった。
    # ワイヤー契約パリティを揃えるため、モデルにvalidateを追加した上でここにテストを足す。
    it "finished_onが過去日なら422 validation_error(他実装とのワイヤー契約パリティ)" do
      post "/internal/v1/tasks", params: { name: "t1", status: "waiting", finished_on: (Date.current - 1).to_s },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:unprocessable_entity)
      body = JSON.parse(response.body)
      expect(body["error"]).to eq("validation_error")
      expect(body["message"]).to include("過去日")
    end

    it "finished_onが今日ちょうどなら許可される(過去日チェックの境界値)" do
      post "/internal/v1/tasks", params: { name: "t1", status: "waiting", finished_on: Date.current.to_s },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:created)
    end

    it "label_idsを渡すとlabelsが紐づく" do
      label = Label.create!(name: "urgent")
      post "/internal/v1/tasks", params: { name: "t1", status: "waiting", finished_on: "2030-01-01", label_ids: [label.id] },
                                  headers: bearer_for(user)
      expect(response).to have_http_status(:created)
      expect(JSON.parse(response.body)["labels"]).to eq([{ "id" => label.id, "name" => "urgent" }])
    end

    # 【テスト監査(9回目、重複label_idsという角度)で発見・修正した実バグ】
    # 同じlabel_idを複数回渡すと(例: [urgent.id, urgent.id])、`assign_labels`が
    # 重複除去せずそのまま`task.label_ids=`へ渡していたため、task_labelsの
    # (task_id,label_id)へのUNIQUE制約(migrations/000004)に違反し、生の
    # `ActiveRecord::RecordNotUnique`が未捕捉のまま500になっていた
    # (Go/Rust/Scala(http4s)実装で見つかった同種のバグと同じ根本原因。
    # Scala(Pekko)は元々`labelIds.distinct`していたため影響を受けていなかった)
    it "同じlabel_idを重複して渡しても500にならず、重複除去して1件だけ紐づく" do
      label = Label.create!(name: "dup-urgent")
      post "/internal/v1/tasks",
           params: { name: "t1", status: "waiting", finished_on: "2030-01-01", label_ids: [label.id, label.id] },
           headers: bearer_for(user)
      expect(response).to have_http_status(:created)
      body = JSON.parse(response.body)
      expect(body["labels"]).to eq([{ "id" => label.id, "name" => "dup-urgent" }])
      expect(TaskLabel.where(task_id: body["id"], label_id: label.id).count).to eq(1)
    end
  end

  describe "GET/PATCH/DELETE /internal/v1/tasks/:id" do
    it "自分のtaskはGET/PATCH/DELETEできる" do
      task = user.tasks.create!(name: "t1", status: "waiting", finished_on: Date.today)

      get "/internal/v1/tasks/#{task.id}", headers: bearer_for(user)
      expect(response).to have_http_status(:ok)

      patch "/internal/v1/tasks/#{task.id}", params: { name: "t1-upd", status: "completed", finished_on: "2030-02-02" },
                                              headers: bearer_for(user)
      expect(response).to have_http_status(:ok)
      expect(JSON.parse(response.body)["name"]).to eq("t1-upd")

      delete "/internal/v1/tasks/#{task.id}", headers: bearer_for(user)
      expect(response).to have_http_status(:no_content)
      expect(TaskRecord.exists?(task.id)).to be false

      # 【6回目のテスト監査(DELETE冪等性のクロス言語パリティ角度)で追加】
      # 既に削除済みのidへ再度DELETEを投げても、find_by(id:)がnilを返し
      # クラッシュせず一貫して404になることを確認する(他4言語もrows-affected/find_byの
      # いずれかで同じ「見つからない」判定に揃っていることを別途確認済み)
      delete "/internal/v1/tasks/#{task.id}", headers: bearer_for(user)
      expect(response).to have_http_status(:not_found)
      expect(JSON.parse(response.body)).to eq("error" => "not_found")
    end

    it "他人のtaskへのGETは404 not_found(認可漏れの防止)" do
      task = other_user.tasks.create!(name: "not-mine", status: "waiting", finished_on: Date.today)
      get "/internal/v1/tasks/#{task.id}", headers: bearer_for(user)
      expect(response).to have_http_status(:not_found)
      expect(JSON.parse(response.body)).to eq("error" => "not_found")
    end

    it "存在しないidへのGETは404 not_found" do
      get "/internal/v1/tasks/999999", headers: bearer_for(user)
      expect(response).to have_http_status(:not_found)
    end

    it "PATCHでfinished_onを過去日に変更しようとすると422 validation_error" do
      task = user.tasks.create!(name: "t1", status: "waiting", finished_on: Date.tomorrow)
      patch "/internal/v1/tasks/#{task.id}",
            params: { name: "t1", status: "waiting", finished_on: (Date.current - 1).to_s },
            headers: bearer_for(user)
      expect(response).to have_http_status(:unprocessable_entity)
      expect(JSON.parse(response.body)["error"]).to eq("validation_error")
    end
  end

  describe "未プロビジョニングのユーザー(admin削除後のセッション相当)" do
    it "JWTは有効だがusersに該当行が無ければ403 user_not_provisioned" do
      token = JWT.encode(
        { sub: "999999", iss: JwtVerifier::LOCAL_HMAC_ISSUER, aud: "backend", exp: 1.hour.from_now.to_i },
        hmac_secret, "HS256"
      )
      get "/internal/v1/tasks", headers: { "Authorization" => "Bearer #{token}" }
      expect(response).to have_http_status(:forbidden)
      expect(JSON.parse(response.body)).to eq("error" => "user_not_provisioned")
    end
  end
end
