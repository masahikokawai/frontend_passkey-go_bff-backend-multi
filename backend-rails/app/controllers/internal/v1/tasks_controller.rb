# CONTRACT.mdセクション5.1のJSON契約に合わせたREST v1相当のTask CRUD
# backend/internal/handler/v1/task.go のRails版(ワイヤー契約は完全一致させる)
module Internal
  module V1
    class TasksController < ApplicationController
      before_action :authenticate!

      DEFAULT_LIMIT = 20

      def index
        if params[:status].present? && !TaskRecord.statuses.key?(params[:status])
          render json: { error: "invalid_status" }, status: :unprocessable_entity
          return
        end

        scope = current_user.tasks.includes(:labels)
        scope = scope.where("name LIKE ?", "%#{params[:name]}%") if params[:name].present?
        scope = scope.where(status: params[:status]) if params[:status].present?
        if params[:label_ids].present?
          label_ids = Array(params[:label_ids]).map(&:to_i)
          scope = scope.joins(:task_labels).where(task_labels: { label_id: label_ids }).distinct
        end

        total = scope.count
        limit = (params[:limit].presence || DEFAULT_LIMIT).to_i
        offset = (params[:offset].presence || 0).to_i

        scope = params[:sort].present? ? scope.order(finished_on: params[:sort].to_sym) : scope.order(:id)
        tasks = scope.limit(limit).offset(offset)

        render json: { tasks: tasks.map(&:as_contract_json), total: total, limit: limit, offset: offset }
      end

      def show
        task = current_user.tasks.find(params[:id])
        render json: task.as_contract_json
      end

      def create
        body = required_task_body
        return unless body

        finished_on = parse_finished_on(body[:finished_on])
        return unless finished_on

        task = current_user.tasks.new(
          name: body[:name], description: body[:description],
          status: body[:status], finished_on: finished_on
        )

        if task.save
          # 【実装中に発覚した実際の挙動】
          # has_many :through(labels) の label_ids= を未保存(new_record?)のレコードに対して行うと、
          # belongs_to :task(required)の検証タイミングの都合で「Task labels is invalid」となり失敗する
          # (Go/Rust/Scala実装には無い、ActiveRecordのhas_many :through特有の制約)
          # そのため先にtask自体を保存し、永続化された後でlabelsを紐付け直す
          assign_labels(task, body[:label_ids])
          render json: task.as_contract_json, status: :created
        else
          render_validation_error(task)
        end
      end

      def update
        task = current_user.tasks.find(params[:id])
        body = required_task_body
        return unless body

        finished_on = parse_finished_on(body[:finished_on])
        return unless finished_on

        task.assign_attributes(
          name: body[:name], description: body[:description],
          status: body[:status], finished_on: finished_on
        )
        assign_labels(task, body[:label_ids])

        if task.save
          render json: task.as_contract_json
        else
          render_validation_error(task)
        end
      end

      def destroy
        task = current_user.tasks.find(params[:id])
        task.destroy!
        head :no_content
      end

      private

      # POST/PATCH 共通のリクエストボディ検証
      # name/status/finished_onは必須
      # (CONTRACT.mdセクション5.1)
      # status自体の妥当性はparse_finished_on同様、
      # ここでは見ずTask.enumのバリデーション(render_validation_error経由)に委ねず、
      # 明示的に invalid_status として400系より前段で弾く(Go実装のstatus検証と揃える)
      def required_task_body
        name = params[:name]
        status = params[:status]
        finished_on = params[:finished_on]
        if name.blank? || status.blank? || finished_on.blank?
          render json: { error: "invalid_request" }, status: :bad_request
          return nil
        end
        unless TaskRecord.statuses.key?(status)
          render json: { error: "invalid_status" }, status: :unprocessable_entity
          return nil
        end
        {
          name: name, description: params[:description], status: status,
          finished_on: finished_on, label_ids: params[:label_ids]
        }
      end

      def parse_finished_on(raw)
        Date.strptime(raw, "%Y-%m-%d")
      rescue ArgumentError, TypeError
        render json: { error: "invalid_finished_on" }, status: :unprocessable_entity
        nil
      end

      def assign_labels(task, label_ids)
        return if label_ids.blank?

        # 【テスト監査で発見・修正した実バグ】label_idsに同じidが重複して含まれる場合
        # (例: [3,3,5])、重複除去せず`label_ids=`(has_many :throughのコレクション代入)に
        # 渡すと、task_labelsの(task_id,label_id)へのUNIQUE制約(migrations/000004)に
        # 違反し、生の`ActiveRecord::RecordNotUnique`がそのまま伝播してしまう
        # (Go/Rust/Scala(http4s)実装で見つかった同種のバグと同じ根本原因。
        # `find`によるレコード解決の段階では重複idも「実在確認」としては素通りし、
        # 後続の中間テーブルへのINSERTで初めて重複が問題になる)
        task.label_ids = Array(label_ids).map(&:to_i).uniq
      end
    end
  end
end
