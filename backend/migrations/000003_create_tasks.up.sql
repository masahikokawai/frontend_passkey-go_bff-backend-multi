-- Rails db/schema.rb の tasks テーブルの写し(training-go/gin/migrations/000005と同一)。
-- status は Task.enum(waiting: 1, work_in_progress: 2, completed: 3) 相当。
CREATE TABLE tasks (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name        VARCHAR(20)     NOT NULL,
    description TEXT            NULL,
    status      TINYINT UNSIGNED NOT NULL DEFAULT 1,
    finished_on DATE            NOT NULL,
    user_id     BIGINT UNSIGNED NOT NULL,
    created_at  DATETIME        NOT NULL,
    updated_at  DATETIME        NOT NULL,
    PRIMARY KEY (id),
    KEY index_tasks_on_finished_on (finished_on),
    KEY index_tasks_on_status (status),
    KEY index_tasks_on_user_id (user_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;
