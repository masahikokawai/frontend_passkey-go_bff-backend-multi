# FeatureFlagsController は Go+Gin実装(admin/go)と同じ機能を提供するRails版
# CRUD実装の比較学習が目的のため、あえて同じ画面構成にしている
class FeatureFlagsController < ApplicationController
  before_action :set_feature_flag, only: %i[edit update audit_logs]

  def index
    @feature_flags = FeatureFlag.order(:flag_key)
  end

  def edit
  end

  def update
    before_enabled = @feature_flag.enabled
    before_default_variation = @feature_flag.default_variation

    if @feature_flag.update(feature_flag_params)
      FeatureFlagAuditLog.create!(
        feature_flag_id: @feature_flag.id,
        flag_key: @feature_flag.flag_key,
        before_enabled: before_enabled,
        after_enabled: @feature_flag.enabled,
        before_default_variation: before_default_variation,
        after_default_variation: @feature_flag.default_variation,
        changed_by: current_admin_name,
        changed_at: Time.current
      )
      redirect_to root_path, flash: { success: "#{@feature_flag.flag_key} を更新しました" }
    else
      flash.now[:danger] = @feature_flag.errors.full_messages.join(", ")
      render :edit, status: :unprocessable_content
    end
  end

  def audit_logs
    @feature_flag_audit_logs = @feature_flag.feature_flag_audit_logs.order(changed_at: :desc)
  end

  private

  def set_feature_flag
    @feature_flag = FeatureFlag.find(params[:id])
  end

  def feature_flag_params
    permitted = params.require(:feature_flag).permit(:default_variation, :enabled)
    # チェックボックスのform_withはunchecked時にキー自体が飛んでこないことがあるため、
    # 明示的にbooleanへ変換する(Railsのcheck_boxヘルパーはhiddenフィールドで"0"を
    # 送るため通常は届くが、テストからの直接POSTでも同じ挙動になるよう安全側に倒す)
    permitted[:enabled] = ActiveModel::Type::Boolean.new.cast(permitted[:enabled]) if permitted.key?(:enabled)
    permitted
  end
end
