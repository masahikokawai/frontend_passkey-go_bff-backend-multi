package com.bffgin.backendpekko

import org.slf4j.LoggerFactory
import slick.jdbc.MySQLProfile.api._
import spray.json._
import Tables._

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.{Executors, TimeUnit}
import scala.concurrent.duration._
import scala.concurrent.{ExecutionContext, Future}

// CONTRACT.mdセクション11・20.7: 外部公開API(/external/v1/tasks)のページネーション方式
// (backend.external-tasks-pagination-v2)は、5言語で共有する1つのflagとして扱う
// backend/internal/featureflag/mysql_retriever.go の BuildFlagConfigJSON と同じ評価規則
// (enabled=falseならcaller側の既定値、有効ならvariations[default_variation]の値)を
// このbool専用フラグ1つに絞って再現する
trait FeatureFlagRepository {
  def getFlag(flagKey: String)(implicit ec: ExecutionContext): Future[Option[FeatureFlagRow]]
}

final class SlickFeatureFlagRepository(db: Database) extends FeatureFlagRepository {
  override def getFlag(flagKey: String)(implicit ec: ExecutionContext): Future[Option[FeatureFlagRow]] =
    db.run(featureFlags.filter(_.flagKey === flagKey).result.headOption)
}

// ExternalTaskRoutesが依存する最小インターフェース。FeatureFlagPollerの実装をテストで
// 差し替えられるようにする(実DB・スケジューラを起動せずに固定値を返すfakeに置き換え可能)
trait FlagSource {
  def currentValue: Boolean
}

object FeatureFlagLogic {

  // Go実装のdefaultBooleanVariations({"on":true,"off":false})と同じフォールバック
  private val defaultBooleanVariations: Map[String, Boolean] = Map("on" -> true, "off" -> false)

  // backend/internal/featureflag/mysql_retriever.go の BuildFlagConfigJSON と同じ評価規則:
  //   - 行が無い、またはenabled=falseなら呼び出し側のdefaultValueを使う
  //   - enabled=trueならvariations(JSON、無ければ{"on":true,"off":false})から
  //     default_variationキーの値を引く。boolでなければ/引けなければdefaultValueへフォールバック
  def resolveBool(row: Option[FeatureFlagRow], defaultValue: Boolean): Boolean = row match {
    case None                          => defaultValue
    case Some(r) if !r.enabled         => defaultValue
    case Some(r) =>
      val variations: Map[String, Boolean] = r.variations.filter(_.nonEmpty) match {
        case Some(json) =>
          try {
            json.parseJson.asJsObject.fields.collect { case (k, JsBoolean(b)) => k -> b }
          } catch {
            case _: Throwable => defaultBooleanVariations
          }
        case None => defaultBooleanVariations
      }
      variations.getOrElse(r.defaultVariation, defaultValue)
  }
}

// 10秒間隔(Goのbackend既定FeatureFlagPollInterval・bffのポーリング間隔と合わせる)で
// feature_flagsテーブルを読み直し、直近の評価結果をAtomicBooleanにキャッシュする
// PekkoのActorSystemスケジューラには依存せず、素朴なjava.util.concurrentのScheduledExecutorService
// を使う(typed/classicどちらのSchedulerにも縛られず、テストでも生成しやすくするため)
final class FeatureFlagPoller(
    repo: FeatureFlagRepository,
    flagKey: String,
    defaultValue: Boolean,
    pollInterval: FiniteDuration = 10.seconds
)(implicit ec: ExecutionContext)
    extends FlagSource {
  private val logger = LoggerFactory.getLogger("FeatureFlagPoller")
  private val cached = new AtomicBoolean(defaultValue)
  private val executor = Executors.newSingleThreadScheduledExecutor()

  private def refresh(): Unit =
    repo.getFlag(flagKey).onComplete {
      case scala.util.Success(row) =>
        val value = FeatureFlagLogic.resolveBool(row, defaultValue)
        val prev = cached.getAndSet(value)
        if (prev != value) {
          logger.info(s"${flagKey}の評価結果が変化しました: $prev -> $value")
        }
      case scala.util.Failure(ex) =>
        logger.warn(s"${flagKey}のポーリングに失敗しました(直前の値を維持): ${ex.getMessage}")
    }

  executor.scheduleWithFixedDelay(() => refresh(), 0, pollInterval.toMillis, TimeUnit.MILLISECONDS)

  override def currentValue: Boolean = cached.get()

  def shutdown(): Unit = executor.shutdown()
}
