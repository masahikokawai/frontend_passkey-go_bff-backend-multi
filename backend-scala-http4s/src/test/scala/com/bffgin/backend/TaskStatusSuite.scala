package com.bffgin.backend

class TaskStatusSuite extends munit.FunSuite {
  test("fromWire: waiting/work_in_progress/completedを正しく変換する") {
    assertEquals(TaskStatus.fromWire("waiting"), Right(TaskStatus.Waiting))
    assertEquals(TaskStatus.fromWire("work_in_progress"), Right(TaskStatus.WorkInProgress))
    assertEquals(TaskStatus.fromWire("completed"), Right(TaskStatus.Completed))
  }

  test("fromWire: 不明な文字列はLeft") {
    assert(TaskStatus.fromWire("unknown").isLeft)
    assert(TaskStatus.fromWire("").isLeft)
  }

  test("fromCode: backend/internal/model/enum.goと同じ数値対応(1/2/3)") {
    assertEquals(TaskStatus.fromCode(1), Right(TaskStatus.Waiting))
    assertEquals(TaskStatus.fromCode(2), Right(TaskStatus.WorkInProgress))
    assertEquals(TaskStatus.fromCode(3), Right(TaskStatus.Completed))
    assert(TaskStatus.fromCode(0).isLeft)
    assert(TaskStatus.fromCode(4).isLeft)
  }

  test("wire文字列とcodeの往復変換が一致する") {
    TaskStatus.all.foreach { s =>
      assertEquals(TaskStatus.fromWire(s.wire), Right(s))
      assertEquals(TaskStatus.fromCode(s.code), Right(s))
    }
  }
}
