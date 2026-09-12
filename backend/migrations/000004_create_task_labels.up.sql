-- training-go/gin/migrations/000006と同じ方針: task_id/label_idともBIGINT UNSIGNEDに統一し、
-- label_id単体のindexも追加する(label_idだけで絞り込むクエリのフルスキャンを防ぐ)。
CREATE TABLE task_labels (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    task_id    BIGINT UNSIGNED NOT NULL,
    label_id   BIGINT UNSIGNED NOT NULL,
    created_at DATETIME        NOT NULL,
    updated_at DATETIME        NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY index_task_label_on_uniq_key (task_id, label_id),
    KEY index_task_labels_on_label_id (label_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;
