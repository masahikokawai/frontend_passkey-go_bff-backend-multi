require "grpc"
require "google/protobuf/well_known_types" # Timestamp#from_time/#to_time用
require "task/v1/task_pb"
require "task/v1/task_services_pb"

# backend/internal/grpcserver/task_service.go の Rails版
# gRPC メタデータの "authorization"(Bearer)から JWT を取り出す点は Go の interceptor.goと同じ
#
# CONTRACT.mdセクション5.1:
# JWT 検証は REST/gRPC 共通ロジックにする、
# という原則を Rails では JwtVerifier/UserResolver を TasksController と共有する形で踏襲している
module GrpcServer
  # 【実機検証で判明した既知の制約への対応】REST(ActionControllerの標準ログ)・外部公開API
  # (同じRailsアプリの別プロセスのため同様に自動でログが出る)には元々ログがあったが、
  # gRPC(bin/grpc_server、素のGRPC::RpcServer)だけはRails.loggerの呼び出しが1つも無く、
  # 実際にどのrpcを処理したかログから確認できなかった
  #
  # 5つのRPCメソッド全てに同じ計測コードを書き足すと二重管理になり、rpc追加時に
  # 書き忘れる恐れがあるため、Module#prependで横断的に計測する
  module RequestLogging
    %i[list_tasks get_task create_task update_task delete_task].each do |rpc_name|
      define_method(rpc_name) do |*args|
        start = Process.clock_gettime(Process::CLOCK_MONOTONIC)
        result = super(*args)
        duration_ms = ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - start) * 1000).round
        # 正常終了 = grpc gemがOKステータス(GRPC::Core::StatusCodes::OK = 0)で応答することと同義
        Rails.logger.info("grpc method=#{rpc_name} status=OK(0) duration_ms=#{duration_ms}")
        result
      rescue GRPC::BadStatus => e
        # 【実機検証で判明・修正】以前は例外クラス名(GRPC::NotFound等)だけを記録しており、
        # 実際にワイヤーへ載るgRPCステータスコードを記録できていなかった(Go/Scala(http4s)より粗い)。
        # GRPC::BadStatus#codeがGRPC::Core::StatusCodesの数値(実際にgrpc gemがクライアントへ
        # 返す値)そのものを持っているため、これを記録するよう修正した
        duration_ms = ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - start) * 1000).round
        Rails.logger.info("grpc method=#{rpc_name} status=#{e.class.name.split('::').last}(#{e.code}) duration_ms=#{duration_ms}")
        raise
      rescue StandardError => e
        # GRPC::BadStatus以外の予期しない例外(bug等)は、grpc gemがUNKNOWN(2)として
        # クライアントへ返す(GRPC::RpcServerが最終的に補足する既定の変換、activeCallの
        # run_server_method参照)ため、ログもそれに合わせる
        duration_ms = ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - start) * 1000).round
        Rails.logger.info("grpc method=#{rpc_name} status=UNKNOWN(2, #{e.class}) duration_ms=#{duration_ms}")
        raise
      end
    end
  end

  class TaskServiceHandler < Task::V1::TaskService::Service
    prepend RequestLogging

    DEFAULT_LIMIT = 20

    def list_tasks(req, call)
      user = resolve_user!(call)

      scope = user.tasks.includes(:labels).order(:id)
      scope = scope.where("name LIKE ?", "%#{req.name}%") if req.name.present?
      if req.status.present?
        raise GRPC::InvalidArgument.new("invalid status: #{req.status}") unless TaskRecord.statuses.key?(req.status)

        scope = scope.where(status: req.status)
      end
      scope = scope.joins(:task_labels).where(task_labels: { label_id: req.label_ids.to_a }).distinct if req.label_ids.any?
      scope = scope.where("tasks.id > ?", req.cursor) if req.cursor.positive?

      limit = req.limit.positive? ? req.limit : DEFAULT_LIMIT
      rows = scope.limit(limit + 1).to_a
      next_cursor = rows.size > limit ? rows[limit - 1].id : 0
      rows = rows.first(limit)

      Task::V1::ListTasksResponse.new(tasks: rows.map { |t| to_pb(t) }, next_cursor: next_cursor)
    rescue UserResolver::UserNotProvisioned
      raise GRPC::PermissionDenied.new("user not provisioned")
    end

    def get_task(req, call)
      user = resolve_user!(call)
      task = user.tasks.find_by(id: req.id)
      raise GRPC::NotFound.new("task not found") unless task

      to_pb(task)
    rescue UserResolver::UserNotProvisioned
      raise GRPC::PermissionDenied.new("user not provisioned")
    end

    def create_task(req, call)
      user = resolve_user!(call)
      finished_on = parse_finished_on!(req.finished_on)

      task = user.tasks.new(
        name: req.name, description: req.description, status: req.status, finished_on: finished_on
      )
      raise GRPC::InvalidArgument.new(task.errors.full_messages.join(", ")) unless task.save

      # app/controllers/internal/v1/tasks_controller.rb の create と同じ理由で、
      # label_idsの割り当ては保存後に行う(has_many :through未保存レコードの制約)
      task.label_ids = req.label_ids.to_a if req.label_ids.any?

      to_pb(task)
    rescue UserResolver::UserNotProvisioned
      raise GRPC::PermissionDenied.new("user not provisioned")
    end

    def update_task(req, call)
      user = resolve_user!(call)
      task = user.tasks.find_by(id: req.id)
      raise GRPC::NotFound.new("task not found") unless task

      finished_on = parse_finished_on!(req.finished_on)
      task.assign_attributes(name: req.name, description: req.description, status: req.status, finished_on: finished_on)
      task.label_ids = req.label_ids.to_a if req.label_ids.any?
      raise GRPC::InvalidArgument.new(task.errors.full_messages.join(", ")) unless task.save

      to_pb(task)
    rescue UserResolver::UserNotProvisioned
      raise GRPC::PermissionDenied.new("user not provisioned")
    end

    def delete_task(req, call)
      user = resolve_user!(call)
      task = user.tasks.find_by(id: req.id)
      raise GRPC::NotFound.new("task not found") unless task

      task.destroy!
      Task::V1::DeleteTaskResponse.new
    rescue UserResolver::UserNotProvisioned
      raise GRPC::PermissionDenied.new("user not provisioned")
    end

    private

    def resolve_user!(call)
      auth = call.metadata["authorization"]
      raise GRPC::Unauthenticated.new("unauthorized") if auth.blank?

      token = auth.start_with?("Bearer ") ? auth.delete_prefix("Bearer ") : nil
      raise GRPC::Unauthenticated.new("unauthorized") if token.blank?

      claims =
        begin
          JwtVerifier.new.verify(token)
        rescue JwtVerifier::VerificationError
          raise GRPC::Unauthenticated.new("invalid_token")
        end
      UserResolver.resolve(claims)
    end

    def parse_finished_on!(raw)
      Date.strptime(raw, "%Y-%m-%d")
    rescue ArgumentError, TypeError
      raise GRPC::InvalidArgument.new("invalid finished_on: #{raw.inspect}")
    end

    def to_pb(task)
      Task::V1::Task.new(
        id: task.id, name: task.name, description: task.description, status: task.status,
        finished_on: task.finished_on.strftime("%Y-%m-%d"),
        labels: task.labels.map { |l| Task::V1::Label.new(id: l.id, name: l.name) },
        created_at: Google::Protobuf::Timestamp.new.from_time(task.created_at.utc),
        updated_at: Google::Protobuf::Timestamp.new.from_time(task.updated_at.utc)
      )
    end
  end
end
