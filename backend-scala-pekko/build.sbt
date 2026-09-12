// CONTRACT.mdセクション20: backendのTask CRUDをScala(Pekko HTTP + pekko-grpc)で比較実装する
//
// 【最重要・ライセンス方針】
// org.apache.pekko(Apache 2.0)のartifactのみを使用する
// com.typesafe.akka(Akka、2022年のBSLライセンス変更で商用制限あり)は一切使わない
// gRPCも pekko-grpc(Apache 2.0継続フォーク)を使い、akka-grpcは使わない
ThisBuild / scalaVersion := "2.13.18"
ThisBuild / version := "0.1.0"
ThisBuild / organization := "com.bffgin"

val pekkoVersion = "1.1.2"
val pekkoHttpVersion = "1.1.0"

// http4s実装(backend-scala-http4s)との対比として、伝統的なPekkoエコシステムの
// 組み合わせ(Slick + spray-json)をあえて選んでいる(CONTRACT.mdセクション20.4)
lazy val root = (project in file("."))
  .enablePlugins(PekkoGrpcPlugin)
  .settings(
    name := "backend-scala-pekko",
    // sbt run をバッチモードで叩くとforkしない設定だとmain()が返った直後にsbt自体が
    // プロセスごと終了してしまい、起動直後にサーバーが道連れで落ちる(非同期bindが完了する前後に関わらず)
    //
    // 別JVM へ fork することで、サーバープロセスの寿命を sbt のコマンド実行寿命から切り離す
    Compile / run / fork := true,
    pekkoGrpcCodeGeneratorSettings += "server_power_apis",
    libraryDependencies ++= Seq(
      "org.apache.pekko" %% "pekko-actor-typed"          % pekkoVersion,
      "org.apache.pekko" %% "pekko-stream"                % pekkoVersion,
      "org.apache.pekko" %% "pekko-http"                  % pekkoHttpVersion,
      "org.apache.pekko" %% "pekko-http-spray-json"       % pekkoHttpVersion,
      "org.apache.pekko" %% "pekko-slf4j"                 % pekkoVersion,
      "com.typesafe.slick" %% "slick"                     % "3.5.2",
      "com.typesafe.slick" %% "slick-hikaricp"             % "3.5.2",
      "com.mysql"          % "mysql-connector-j"           % "9.7.0",
      "com.nimbusds"       % "nimbus-jose-jwt"             % "10.9.1",
      "ch.qos.logback"     % "logback-classic"             % "1.5.38",
      "org.scalatest"     %% "scalatest"                   % "3.2.19" % Test,
      "org.apache.pekko"  %% "pekko-http-testkit"          % pekkoHttpVersion % Test,
      "org.apache.pekko"  %% "pekko-actor-testkit-typed"   % pekkoVersion % Test,
      "org.apache.pekko"  %% "pekko-stream-testkit"        % pekkoVersion % Test
    )
  )
