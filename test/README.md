# 测试目录

## 目录用途

- `frontend/`：前端自动测试及其构建路径约束测试，直接复用 `frontend/node_modules`。
- `backend/`：集中保存邮件监听、账号协议隔离和凭据竞态回归，通过默认入口的 Go overlay 注入相应业务包；不复制测试文件到源码目录。
- `tools/password-check/`：离线密码哈希校验工具，只通过环境变量接收输入，不输出密码和哈希。
- `manual/smtp/`：人工 SMTP 发信脚本，可能向外部发送真实邮件，不包含在自动测试中。运行前需自行检查收发件人和服务器配置。
- `plugins/spam_block/`：垃圾邮件模型测试，需要额外安装 TensorFlow 并准备相应模型；不包含在默认测试中。
- `archive/`：历史测试脚本和补丁，仅留作检查证据，路径和运行环境可能已失效，不应直接执行，也不提交其中的本机配置。
- `run.mjs`：统一自动测试入口。

## 执行方法

在项目根目录运行：

```sh
node test/run.mjs all
node test/run.mjs frontend
node test/run.mjs backend
node test/run.mjs backend -count=1 ./...
node test/run.mjs backend -race ./...
```

首次执行 Go 测试前，需要在 `frontend/` 安装依赖并运行 `npm run build`，为 `go:embed` 提供前端文件。默认命令不会启动开发服务、清空真实数据库或发送真实邮件。

每次运行自动创建 `test-log/run-时间-进程号/`，保存前后端日志及临时文件。子进程的 `SMAIL_ROOT` 和临时目录指向该次测试目录，不继承开发后端的真实配置根目录。MySQL/PostgreSQL 集成测试仍需单独准备专用数据库，不能指向业务数据库。

Windows 密码检查工具的构建命令：

```powershell
Set-Location test/tools/password-check
go build -o ../../../out/test-tools/password-check.exe .
```

## 三种数据库迁移验收

```sh
node test/run.mjs backend -count=1 -v ./services/dbmigration -run TestPortableSQLIntegration
```

网络数据库实测必须先配置专用测试服务的环境变量：

- `SMAIL_TEST_MIGRATION_MYSQL`：结构化连接，`type` 为 `mysql`。
- `SMAIL_TEST_MIGRATION_POSTGRES`：结构化连接，`type` 为 `postgresql`，服务必须支持 TLS。
- 两个变量的内容均为 JSON，字段为 `type`、`host`、`port`、`username`、`password`，其中 `password` 是包含 `plaintext` 字段的对象；不填写 `name` 或 `localRef`，不把真实凭据写入仓库。
- 使用拥有创建数据库权限的专用账号。测试仅创建 `smail_migration_test_` 前缀的随机新库，不访问现有业务库，不自动删除网络测试库。
- 单个服务执行 SQLite → 网络库 → 同类型新库 → SQLite；同时提供两种服务时，额外执行 SQLite → MySQL → PostgreSQL → MySQL → SQLite。
- 逐表校验内容，并检查中文主题、二进制会话、日期、布尔值和新增记录主键。配置切换使用测试回调，不代表完整部署重启验收。
- 未提供连接时显示跳过，不能作为 MySQL/PostgreSQL 实库迁移通过的证据。

## Go 测试源码的必要例外

`backend/` 中的同包 `*_test.go` 保持原位，其中许多测试使用包内私有函数或相对路径。将其机械移动到独立目录会破坏 Go 测试语义，因此只集中运行入口，不改变业务包的可见性和 API。后续跨模块、浏览器和独立协议测试应放到本目录。

截图、测试生成的 Logo 检查图、浏览器配置、测试数据库、报告和日志都放到 `test-log/`；测试工具二进制放到 `out/test-tools/`。运行用 Logo、文档演示图、模型训练脚本及数据不是临时测试产物，不作清理。
