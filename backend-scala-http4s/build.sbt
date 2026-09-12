ThisBuild / scalaVersion := "2.13.15"
ThisBuild / version := "0.1.0"
ThisBuild / organization := "com.bffgin"

enablePlugins(Fs2Grpc)

val http4sVersion = "0.23.30"
val doobieVersion = "1.0.0-RC5"
val circeVersion = "0.14.10"

lazy val root = (project in file("."))
  .settings(
    name := "backend-scala-http4s",
    libraryDependencies ++= Seq(
      "org.http4s" %% "http4s-ember-server" % http4sVersion,
      "org.http4s" %% "http4s-dsl" % http4sVersion,
      "org.http4s" %% "http4s-circe" % http4sVersion,
      "io.circe" %% "circe-generic" % circeVersion,
      "io.circe" %% "circe-parser" % circeVersion,
      "org.tpolecat" %% "doobie-core" % doobieVersion,
      "org.tpolecat" %% "doobie-hikari" % doobieVersion,
      "com.mysql" % "mysql-connector-j" % "9.1.0",
      "com.nimbusds" % "nimbus-jose-jwt" % "9.48",
      "org.typelevel" %% "log4cats-slf4j" % "2.7.0",
      "ch.qos.logback" % "logback-classic" % "1.5.15",
      "io.grpc" % "grpc-netty-shaded" % "1.68.1",
      "com.google.protobuf" % "protobuf-java" % "3.25.5" % "protobuf",
      "org.scalameta" %% "munit" % "1.0.4" % Test,
      "org.typelevel" %% "munit-cats-effect" % "2.0.0" % Test
    ),
    Compile / mainClass := Some("com.bffgin.backend.Main"),
    testFrameworks += new TestFramework("munit.Framework")
  )
