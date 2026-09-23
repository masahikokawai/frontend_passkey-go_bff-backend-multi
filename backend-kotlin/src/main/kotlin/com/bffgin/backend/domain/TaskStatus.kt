package com.bffgin.backend.domain

/** backend(Go)のTaskStatus(waiting=1, work_in_progress=2, completed=3)と同じマッピング */
enum class TaskStatus(val dbValue: Int, val wireValue: String) {
    WAITING(1, "waiting"),
    WORK_IN_PROGRESS(2, "work_in_progress"),
    COMPLETED(3, "completed");

    companion object {
        fun fromWireValue(s: String): TaskStatus? = entries.firstOrNull { it.wireValue == s }
        fun fromDbValue(v: Int): TaskStatus? = entries.firstOrNull { it.dbValue == v }
    }
}
