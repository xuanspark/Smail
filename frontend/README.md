# 前端

前端代码库

```sh
yarn
```

### 开发与热更新

```sh
yarn dev
```

开发环境由前端占用 HTTP `80` 和 HTTPS `443`，后端 API 独立监听 `5201`。前端开发代理默认连接本机后端 `http://127.0.0.1:5201`。如果后端使用其他地址，请在 `frontend/.env.development` 中设置：

```env
VITE_DEV_BACKEND_URL=http://127.0.0.1:5201
```

前端开发服务使用 `config/ssl` 下的证书提供 HTTPS，HTTP `80` 只负责跳转到 HTTPS。生产环境应使用有效证书，并由正式 Web 服务器承载前端入口。

### 生产构建

```sh
yarn build
```

产物写入根目录的 `out/frontend/`，随后由 `deploy/prepare-frontend.mjs` 同步到后端必需的 Go 嵌入目录。不要再手动复制到 `frontend/dist/`。

### 自动测试

```sh
yarn test
```

测试源码位于 `test/frontend/`，通过 `test/run.mjs` 统一执行，日志写入 `test-log/`，无需在测试目录重复安装 Vue 依赖。

### 代码检查 [ESLint](https://eslint.org/)

```sh
yarn lint
```
