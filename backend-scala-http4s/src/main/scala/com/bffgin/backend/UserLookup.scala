package com.bffgin.backend

import cats.effect.IO

// backend/internal/handler/v1/task.go の userLookup インターフェースと同じ意図:
// TaskRepo(doobie/実DB)から必要な最小メソッドだけを切り出し、テストではfakeに差し替えられるようにする
trait UserLookup {
  def findUserById(id: Long): IO[Option[User]]
  def findUserByKeycloakSub(sub: String): IO[Option[User]]
}
