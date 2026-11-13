CREATE TABLE access_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    trace_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '服务端生成的 32 位追踪编号',
    method VARCHAR(16) NOT NULL,
    path VARCHAR(512) NOT NULL COMMENT '匹配的路由模板，不含查询参数或业务路径值',
    status_code SMALLINT UNSIGNED NOT NULL,
    duration_ms BIGINT NOT NULL,
    client_ip VARCHAR(64) NOT NULL,
    created_at BIGINT NOT NULL COMMENT 'Unix 毫秒',
    PRIMARY KEY (id),
    KEY idx_access_trace (trace_id),
    KEY idx_access_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
