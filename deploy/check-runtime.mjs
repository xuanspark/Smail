import { assertDeploymentLayout } from './runtime-config.mjs'

try {
  assertDeploymentLayout()
  console.log('模拟部署配置、证书与账号库路径检查通过。')
} catch (error) {
  console.error(`yunx-test 尚未完成安全迁移，禁止启动：${error.message}`)
  process.exitCode = 1
}
