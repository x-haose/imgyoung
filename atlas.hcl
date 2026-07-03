// Atlas 项目配置:声明式 schema/ 为唯一真相,versioned 迁移由其 diff 生成。
// 用法:atlas migrate diff <name> --env local
env "local" {
  // 期望状态:每表一文件的声明式建表。
  src = "file://internal/infra/db/schema"

  // 计算 diff 用的临时库(内存 SQLite,纯 Go,不落盘)。
  dev = "sqlite://dev?mode=memory"

  migration {
    // 生成的 versioned 迁移落地目录。
    dir = "file://internal/infra/db/migrations"
  }
}
