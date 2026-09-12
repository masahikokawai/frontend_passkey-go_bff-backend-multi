# CONTRACT.mdセクション11(BFF非経由の外部公開API)のRails版。
# backend/internal/handler/external/task.go・authjwt/external_middleware.go 相当。
# 内部CRUD(Internal::V1::TasksController)とは別ミドルウェア(Client Credentials Grant
# 専用、azpクレームの追加チェック)・別ポート(既定:8101、config/puma_external.rb)で
# 待ち受けるため、意図的にコントローラ・ルート名前空間を分けている。
module External
  module V1
    class TasksController < ActionController::API
      before_action :authenticate_external_client!

      DEFAULT_PAGE_SIZE = 10
      DEFAULT_LIMIT = 10

      def index
        user_id = params[:user_id]
        if user_id.blank?
          render json: { error: "user_id is required" }, status: :bad_request
          return
        end
        user_id = Integer(user_id, exception: false)
        if user_id.nil?
          render json: { error: "invalid user_id" }, status: :bad_request
          return
        end

        if ExternalPaginationFlag.v2_enabled?
          list_v2(user_id)
        else
          list_v1(user_id)
        end
      end

      private

      # offsetページング(Feature Flag OFF、既定)。created_at DESC, id DESC で安定ソート
      # (backend/internal/repository/task.go の ListOffsetForExternalAPI と同一)。
      def list_v1(user_id)
        page = positive_int(params[:page], default: 1)
        page_size = positive_int(params[:page_size], default: DEFAULT_PAGE_SIZE)

        scope = TaskRecord.where(user_id: user_id).includes(:labels)
        total = scope.count
        tasks = scope.order(created_at: :desc, id: :desc).limit(page_size).offset((page - 1) * page_size)

        render json: {
          tasks: tasks.map(&:as_external_contract_json),
          page: page,
          page_size: page_size,
          total: total
        }
      end

      # keyset(cursor)ページング(Feature Flag ON)。次ページが無ければnext_cursor=nil
      # (backend/internal/repository/task.go の ListCursorForExternalAPI と同一)。
      def list_v2(user_id)
        limit = positive_int(params[:limit], default: DEFAULT_LIMIT)

        scope = TaskRecord.where(user_id: user_id).includes(:labels)
        if params[:cursor].present?
          begin
            pos = ExternalCursor.decode(params[:cursor])
          rescue ExternalCursor::InvalidCursor => e
            render json: { error: "invalid_cursor", message: e.message }, status: :unprocessable_entity
            return
          end
          scope = scope.where(
            "(created_at < ?) OR (created_at = ? AND id < ?)",
            pos.created_at, pos.created_at, pos.id
          )
        end

        tasks = scope.order(created_at: :desc, id: :desc).limit(limit).to_a

        next_cursor = nil
        if tasks.length == limit
          last = tasks.last
          next_cursor = ExternalCursor.encode(last.created_at, last.id)
        end

        render json: { tasks: tasks.map(&:as_external_contract_json), next_cursor: next_cursor, limit: limit }
      end

      def positive_int(raw, default:)
        n = raw.presence && Integer(raw, exception: false)
        n && n >= 1 ? n : default
      end

      def authenticate_external_client!
        header = request.headers["Authorization"].to_s
        token = header.start_with?("Bearer ") ? header.delete_prefix("Bearer ") : nil
        if token.blank?
          render json: { error: "unauthorized" }, status: :unauthorized
          return
        end

        claims =
          begin
            jwt_verifier.verify(token)
          rescue JwtVerifier::VerificationError
            render json: { error: "invalid_token" }, status: :unauthorized
            return
          end

        expected_client_id = ENV.fetch("EXTERNAL_API_CLIENT_ID", "external-api-client")
        if claims.azp != expected_client_id
          # 既知の制約(Go実装と同一、CONTRACT.mdセクション11参照): azpが一致する限り
          # 任意のuser_idを指定してタスクを読める。エンドユーザー単位の認可は行わない、
          # サーバー間の信頼関係を前提にした設計。
          render json: { error: "client_not_allowed" }, status: :forbidden
          return
        end
      end

      def jwt_verifier
        @jwt_verifier ||= JwtVerifier.new
      end
    end
  end
end
