import { createRequire } from 'node:module'

// 共用前端依赖，不在测试目录安装第二份 Vue。
const require = createRequire(new URL('../../frontend/package.json', import.meta.url))
export const { computed, reactive, ref } = require('vue')
