require "rails_helper"
require "grpc_server/task_service"

# CONTRACT.mdセクション20.10・20.11: gRPC(bin/grpc_server)のリクエスト単位ログ(GrpcServer::RequestLogging)は
# これまでテストが1件も無く、ruby -cの構文チェックとbin/rails runnerでのprepend配線確認のみで
# 実際にRails.loggerへ何が記録されるかは未検証だった。
#
# 特に「GRPC::BadStatus#codeが実際にワイヤーへ載る値と一致しているか」「BadStatus以外の例外を
# UNKNOWN(2)として記録しているか」は、grpc gem自身の変換ロジック(rpc_desc.rb)と整合しているかが
# 実装の正しさそのものに関わるため、ここで実際にRails.loggerへの出力を検証する
RSpec.describe GrpcServer::TaskServiceHandler do
  let(:hmac_secret) { "test-hmac-secret" }
  let!(:user) { User.create!(keycloak_sub: "sub-1", email: "u@example.com", name: "U", role: 1) }
  let(:handler) { described_class.new }
  let(:logged_messages) { [] }

  before do
    allow(JwtVerifier).to receive(:new).and_return(
      JwtVerifier.new(local_hmac_secret: hmac_secret, expected_audience: "backend")
    )
    # 【実装上の注意】RequestLoggingはRails.logger.infoを1回だけ呼ぶため、
    # メッセージそのものを配列へ集めてアサーションする(標準出力を奪う必要が無い)
    allow(Rails.logger).to receive(:info) { |msg| logged_messages << msg }
  end

  def call_for(u)
    token = JWT.encode(
      { sub: u.id.to_s, iss: JwtVerifier::LOCAL_HMAC_ISSUER, aud: "backend", exp: 1.hour.from_now.to_i },
      hmac_secret, "HS256"
    )
    double("grpc_call", metadata: { "authorization" => "Bearer #{token}" })
  end

  describe "正常終了" do
    it "status=OK(0)をmethod=list_tasksで記録する" do
      handler.list_tasks(Task::V1::ListTasksRequest.new, call_for(user))

      expect(logged_messages).to include(a_string_matching(/\Agrpc method=list_tasks status=OK\(0\) duration_ms=\d+\z/))
    end
  end

  describe "GRPC::BadStatusを送出した場合" do
    it "get_taskで存在しないtask_idを指定するとGRPC::NotFound(実際のgRPCステータスコード5)が記録される" do
      expect do
        handler.get_task(Task::V1::GetTaskRequest.new(id: 999_999), call_for(user))
      end.to raise_error(GRPC::NotFound)

      # 【実機検証で判明・修正した箇所の回帰確認】e.codeがGRPC::Core::StatusCodes::NOT_FOUND(=5)と
      # 一致していること。以前は例外クラス名(GRPC::NotFound)のみでこの数値を記録していなかった
      expect(GRPC::Core::StatusCodes::NOT_FOUND).to eq(5)
      expect(logged_messages).to include(a_string_matching(/\Agrpc method=get_task status=NotFound\(5\) duration_ms=\d+\z/))
    end

    it "権限の無いユーザー由来のclaimsではGRPC::PermissionDenied(7)が記録される" do
      allow(UserResolver).to receive(:resolve).and_raise(UserResolver::UserNotProvisioned, "not provisioned")

      expect do
        handler.list_tasks(Task::V1::ListTasksRequest.new, call_for(user))
      end.to raise_error(GRPC::PermissionDenied)

      expect(logged_messages).to include(a_string_matching(/\Agrpc method=list_tasks status=PermissionDenied\(7\) duration_ms=\d+\z/))
    end
  end

  describe "GRPC::BadStatus以外の予期しない例外" do
    it "UNKNOWN(2)として記録し、元の例外はそのまま呼び出し元へ再送出する(握りつぶさない)" do
      allow(UserResolver).to receive(:resolve).and_raise(StandardError, "boom")

      expect do
        handler.list_tasks(Task::V1::ListTasksRequest.new, call_for(user))
      end.to raise_error(StandardError, "boom")

      # 【grpc gem自身の変換ロジック(rpc_desc.rb)との整合確認】
      # GRPC::BadStatus以外のStandardErrorは、grpc gem自体がUNKNOWN(2)としてクライアントへ
      # 応答する(rescue StandardError, NotImplementedError => e; send_status(active_call, UNKNOWN, ...))。
      # ログ側の実装(UNKNOWN(2)固定)がこの実際の挙動と食い違っていないことを確認する
      expect(GRPC::Core::StatusCodes::UNKNOWN).to eq(2)
      expect(logged_messages).to include(a_string_matching(/\Agrpc method=list_tasks status=UNKNOWN\(2, StandardError\) duration_ms=\d+\z/))
    end
  end

  describe "durationの計測" do
    # 【define_method内でrescueするコードのスコープ確認】
    # startはdefine_method(:list_tasks)のブロック引数スコープで定義され、rescue節からも
    # 参照できる(Ruby 2.6+のdo...end内rescueはメソッド本体と同じローカル変数スコープを共有する)。
    # 正常系・異常系ともに同じ経過時間を計測できていることを、複数回呼び出しても毎回
    # duration_msが記録される(NameError等でクラッシュしない)ことで確認する
    it "正常系・異常系のいずれでも複数回連続して呼び出してもduration_msが記録され続ける" do
      3.times { handler.list_tasks(Task::V1::ListTasksRequest.new, call_for(user)) }
      3.times do
        expect { handler.get_task(Task::V1::GetTaskRequest.new(id: 999_999), call_for(user)) }.to raise_error(GRPC::NotFound)
      end

      ok_logs = logged_messages.grep(/method=list_tasks status=OK/)
      error_logs = logged_messages.grep(/method=get_task status=NotFound/)
      expect(ok_logs.size).to eq(3)
      expect(error_logs.size).to eq(3)
    end
  end
end
