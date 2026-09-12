package com.bffgin.backend.grpc

import io.grpc._

// LoggingServerInterceptor(gRPCのリクエスト単位ログ)の回帰テスト。
//
// 【テスト監査で追加】このinterceptorはMain.scalaでServerInterceptors.interceptを通じてのみ
// 使われており、以前は単体テストが1件も無かった。ServerCall#closeをラップして
// 実際のgRPCステータス(io.grpc.Status)を記録する実装だが、ラップの過程で
// 「実際にクライアントへ返るstatus/trailersが変わってしまう」という致命的な回帰
// (=ログを足しただけのつもりが挙動を変えてしまう)が最も警戒すべき点のため、
// 実際のServerCallを使わず、呼ばれた引数を記録するだけの最小限のfakeで検証する
// (grpc-testing/InProcess transportは使わず、interceptCall自体を直接呼ぶユニットテスト)
class LoggingServerInterceptorSuite extends munit.FunSuite {

  // マーシャラは実際には使わない(sendMessage/requestを呼ばないテストのため)が、
  // MethodDescriptorの構築に必須なので最小限のダミーを用意する
  private val stringMarshaller = new MethodDescriptor.Marshaller[String] {
    override def stream(value: String): java.io.InputStream =
      new java.io.ByteArrayInputStream(value.getBytes("UTF-8"))
    override def parse(stream: java.io.InputStream): String =
      new String(stream.readAllBytes(), "UTF-8")
  }

  private def methodDescriptor(fullName: String): MethodDescriptor[String, String] =
    MethodDescriptor
      .newBuilder(stringMarshaller, stringMarshaller)
      .setType(MethodDescriptor.MethodType.UNARY)
      .setFullMethodName(fullName)
      .build()

  // 実際のネットワーク・シリアライズを一切行わない、close呼び出しの引数だけを記録するfake
  private class RecordingServerCall(desc: MethodDescriptor[String, String]) extends ServerCall[String, String] {
    var closedWith: Option[(Status, Metadata)] = None
    override def request(numMessages: Int): Unit = ()
    override def sendHeaders(headers: Metadata): Unit = ()
    override def sendMessage(message: String): Unit = ()
    override def isReady: Boolean = true
    override def isCancelled: Boolean = false
    override def close(status: Status, trailers: Metadata): Unit = { closedWith = Some((status, trailers)) }
    override def getMethodDescriptor: MethodDescriptor[String, String] = desc
  }

  test("interceptCallで包んだ後closeしても、実際にクライアントへ渡るstatus/trailersは変わらない(ログのための副作用が本体の挙動を変えない)") {
    val desc = methodDescriptor("task.v1.TaskService/GetTask")
    val realCall = new RecordingServerCall(desc)
    val headers = new Metadata()
    val interceptor = new LoggingServerInterceptor()

    val handler = new ServerCallHandler[String, String] {
      override def startCall(call: ServerCall[String, String], headers: Metadata): ServerCall.Listener[String] = {
        // ハンドラの中で実際に(NOT_FOUND等の)ステータスでcloseする、という
        // 本物のRPC処理を模す
        call.close(Status.NOT_FOUND.withDescription("task not found"), new Metadata())
        new ServerCall.Listener[String] {}
      }
    }

    interceptor.interceptCall(realCall, headers, handler)

    val (status, _) = realCall.closedWith.getOrElse(fail("closeが呼ばれていない"))
    assertEquals(status.getCode, Status.Code.NOT_FOUND, "ラップ前後でステータスコードが変わっている(実害: クライアントへ誤った結果が返る)")
    assertEquals(status.getDescription, "task not found", "ラップ前後でエラーメッセージが変わっている")
  }

  test("成功時(Status.OK)もそのまま透過する") {
    val desc = methodDescriptor("task.v1.TaskService/ListTasks")
    val realCall = new RecordingServerCall(desc)
    val interceptor = new LoggingServerInterceptor()

    val handler = new ServerCallHandler[String, String] {
      override def startCall(call: ServerCall[String, String], headers: Metadata): ServerCall.Listener[String] = {
        call.close(Status.OK, new Metadata())
        new ServerCall.Listener[String] {}
      }
    }

    interceptor.interceptCall(realCall, new Metadata(), handler)

    val (status, _) = realCall.closedWith.getOrElse(fail("closeが呼ばれていない"))
    assertEquals(status.getCode, Status.Code.OK)
  }

  test("あらゆるgRPCステータスコードでログ出力(close呼び出し)がpanicしない") {
    val desc = methodDescriptor("task.v1.TaskService/DeleteTask")
    val allCodes = Status.Code.values().toList
    allCodes.foreach { code =>
      val realCall = new RecordingServerCall(desc)
      val interceptor = new LoggingServerInterceptor()
      val handler = new ServerCallHandler[String, String] {
        override def startCall(call: ServerCall[String, String], headers: Metadata): ServerCall.Listener[String] = {
          call.close(Status.fromCode(code), new Metadata())
          new ServerCall.Listener[String] {}
        }
      }
      interceptor.interceptCall(realCall, new Metadata(), handler)
      assertEquals(realCall.closedWith.map(_._1.getCode), Some(code))
    }
  }
}
