-- Rails db/schema.rb の labels テーブルの写し(training-go/gin/migrations/000004と同一)。
CREATE TABLE labels (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name       VARCHAR(255)    NOT NULL,
    created_at DATETIME        NOT NULL,
    updated_at DATETIME        NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY index_labels_on_name (name)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;
