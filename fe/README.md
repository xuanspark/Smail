# fe

前端代码库

```sh
yarn
```

### Compile and Hot-Reload for Development

```sh
yarn dev
```

开发代理默认连接本机 HTTPS 后端 `https://127.0.0.1:443`。如果后端使用其他地址，请在 `fe/.env.development` 中设置：

```env
VITE_DEV_BACKEND_URL=https://127.0.0.1:443
```

自签名证书仅用于本地开发，代理不会校验证书链；生产环境应使用有效证书。

### Compile and Minify for Production

```sh
yarn build
```

### Lint with [ESLint](https://eslint.org/)

```sh
yarn lint
```
