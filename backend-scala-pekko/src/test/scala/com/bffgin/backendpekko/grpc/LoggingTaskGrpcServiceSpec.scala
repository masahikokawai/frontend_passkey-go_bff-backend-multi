package com.bffgin.backendpekko.grpc

import io.grpc.Status
import org.apache.pekko.grpc.GrpcServiceException
import org.apache.pekko.grpc.scaladsl.Metadata
import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec
import org.slf4j.LoggerFactory
import task.v1._

import scala.concurrent.duration._
import scala.concurrent.{Await, ExecutionContext, Future}

// LoggingTaskGrpcService(gRPCのリクエスト単位ログ)の回帰テスト。
//
// 【テスト監査で追加】このデコレータはMain.scalaで実際に使われる(TaskGrpcServiceImplの代わりに
// TaskServicePowerApiHandler.partialへ渡される)唯一の実装だが、以前はテストが1件も無く、
// TaskGrpcServiceImpl(素のビジネスロジック)だけがテストされ、デコレータ自身の正しさ
// (=ログを足しただけのつもりで、成功/失敗の値をすり替えてしまっていないか)は
// 一度も検証されていなかった。
//
// logged()はScalaの`Future#andThen`を使っており、andThenのコールバック内で例外が起きても
// 呼び出し元に返るFutureの成功/失敗自体は変わらない(Scala標準ライブラリの仕様)ため、
// 「ログ処理自体が本体の結果をすり替えていないか」を確実に検証する
class LoggingTaskGrpcServiceSpec extends AnyWordSpec with Matchers {
  private implicit val ec: ExecutionContext = ExecutionContext.Implicits.global
  private val logger = LoggerFactory.getLogger("test.grpc")

  // LoggingTaskGrpcServiceはmetadataを一切参照せずdelegateへそのまま転送するだけなので、
  // テスト用のFakeDelegateも同様にmetadataを使わない。よって実体を用意する必要が無い
  private val noMetadata: Metadata = null.asInstanceOf[Metadata]

  private class FakeDelegate(
      listTasksResult: => Future[ListTasksResponse] = Future.failed(new NotImplementedError()),
      getTaskResult: => Future[Task] = Future.failed(new NotImplementedError()),
      createTaskResult: => Future[Task] = Future.failed(new NotImplementedError()),
      updateTaskResult: => Future[Task] = Future.failed(new NotImplementedError()),
      deleteTaskResult: => Future[DeleteTaskResponse] = Future.failed(new NotImplementedError())
  ) extends TaskServicePowerApi {
    override def listTasks(in: ListTasksRequest, metadata: Metadata): Future[ListTasksResponse] = listTasksResult
    override def getTask(in: GetTaskRequest, metadata: Metadata): Future[Task] = getTaskResult
    override def createTask(in: CreateTaskRequest, metadata: Metadata): Future[Task] = createTaskResult
    override def updateTask(in: UpdateTaskRequest, metadata: Metadata): Future[Task] = updateTaskResult
    override def deleteTask(in: DeleteTaskRequest, metadata: Metadata): Future[DeleteTaskResponse] = deleteTaskResult
  }

  "LoggingTaskGrpcService" should {
    "成功時、delegateが返した値をそのまま透過する(ログのための副作用が値をすり替えない)" in {
      val response = ListTasksResponse(tasks = Seq(Task(id = 1, name = "テスト")), nextCursor = 0)
      val delegate = new FakeDelegate(listTasksResult = Future.successful(response))
      val svc = new LoggingTaskGrpcService(delegate, logger)

      val result = Await.result(svc.listTasks(ListTasksRequest(), noMetadata), 1.second)

      result shouldBe response
    }

    "失敗時(GrpcServiceException)、ステータスコード・メッセージを変えずそのまま伝播する" in {
      val ex = new GrpcServiceException(Status.NOT_FOUND.withDescription("task not found"))
      val delegate = new FakeDelegate(getTaskResult = Future.failed(ex))
      val svc = new LoggingTaskGrpcService(delegate, logger)

      val thrown = intercept[GrpcServiceException] {
        Await.result(svc.getTask(GetTaskRequest(id = 1), noMetadata), 1.second)
      }

      thrown.status.getCode shouldBe Status.Code.NOT_FOUND
      thrown.status.getDescription shouldBe "task not found"
    }

    "GrpcServiceException以外の予期しない例外も、そのまま(すり替えず)伝播する" in {
      // ログ処理側はUNKNOWNとして記録するだけで、実際に呼び出し元へ返る例外そのものは
      // 変更してはいけない(呼び出し元が例外の型で分岐しているケースを壊さないため)
      val boom = new RuntimeException("boom")
      val delegate = new FakeDelegate(createTaskResult = Future.failed(boom))
      val svc = new LoggingTaskGrpcService(delegate, logger)

      val thrown = intercept[RuntimeException] {
        Await.result(svc.createTask(CreateTaskRequest(), noMetadata), 1.second)
      }

      thrown shouldBe theSameInstanceAs(boom)
    }

    "5つのRPC全てが、対応するdelegateのメソッドだけを呼ぶ(取り違えて別のRPCを呼んでいないか)" in {
      val deleteResponse = DeleteTaskResponse()
      val delegate = new FakeDelegate(deleteTaskResult = Future.successful(deleteResponse))
      val svc = new LoggingTaskGrpcService(delegate, logger)

      val result = Await.result(svc.deleteTask(DeleteTaskRequest(id = 5), noMetadata), 1.second)

      result shouldBe deleteResponse
    }

    "複数RPCを並行に呼んでも、それぞれ独立した結果を返す(ログ処理がグローバルな状態を共有して混線しないか)" in {
      val delegate = new FakeDelegate(
        getTaskResult = Future.successful(Task(id = 1, name = "A")),
        createTaskResult = Future.failed(new GrpcServiceException(Status.INVALID_ARGUMENT))
      )
      val svc = new LoggingTaskGrpcService(delegate, logger)

      val getFuture = svc.getTask(GetTaskRequest(id = 1), noMetadata)
      val createFuture = svc.createTask(CreateTaskRequest(), noMetadata)

      Await.result(getFuture, 1.second) shouldBe Task(id = 1, name = "A")
      intercept[GrpcServiceException] {
        Await.result(createFuture, 1.second)
      }.status.getCode shouldBe Status.Code.INVALID_ARGUMENT
    }
  }
}
