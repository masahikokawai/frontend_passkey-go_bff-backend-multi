package com.bffgin.backend.domain;

import java.util.Optional;

/** backend(Go)のTaskStatus(waiting=1, work_in_progress=2, completed=3)と同じマッピング */
public enum TaskStatus {
    WAITING(1, "waiting"),
    WORK_IN_PROGRESS(2, "work_in_progress"),
    COMPLETED(3, "completed");

    private final int dbValue;
    private final String wireValue;

    TaskStatus(int dbValue, String wireValue) {
        this.dbValue = dbValue;
        this.wireValue = wireValue;
    }

    public int dbValue() {
        return dbValue;
    }

    public String wireValue() {
        return wireValue;
    }

    public static Optional<TaskStatus> fromWireValue(String s) {
        for (TaskStatus status : values()) {
            if (status.wireValue.equals(s)) {
                return Optional.of(status);
            }
        }
        return Optional.empty();
    }

    public static Optional<TaskStatus> fromDbValue(int v) {
        for (TaskStatus status : values()) {
            if (status.dbValue == v) {
                return Optional.of(status);
            }
        }
        return Optional.empty();
    }
}
