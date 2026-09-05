# 测试目录

## 目录用途

- `frontend/`：前端自动测试及其构建路径约束测试，直接复用 `frontend/node_modules`。
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

## Go 测试源码的必要例外

`backend/` 中的同包 `*_test.go` 保持原位，其中许多测试使用包内私有函数或相对路径。将其机械移动到独立目录会破坏 Go 测试语义，因此只集中运行入口，不改变业务包的可见性和 API。后续跨模块、浏览器和独立协议测试应放到本目录。

截图、测试生成的 Logo 检查图、浏览器配置、测试数据库、报告和日志都放到 `test-log/`；测试工具二进制放到 `out/test-tools/`。运行用 Logo、文档演示图、模型训练脚本及数据不是临时测试产物，不作清理。
