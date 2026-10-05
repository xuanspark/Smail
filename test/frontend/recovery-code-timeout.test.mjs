import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref, watch } from './vue.mjs'
import { RECOVERY_CODE_DISPLAY_MS } from '../../frontend/src/utils/recoveryCodeDisplay.js'

// 直接执行页面逻辑并注入时钟；该回归不替代部署后的浏览器完整计时验收。
function createClock() {
  let now = 0
  let nextID = 0
  const timers = new Map()
  return {
    setTimeout(callback, delay) {
      const id = ++nextID
      timers.set(id, { callback, deadline: now + delay })
      return id
    },
    clearTimeout(id) { timers.delete(id) },
    advance(milliseconds) {
      now += milliseconds
      for (const [id, timer] of timers) {
        if (timer.deadline <= now) {
          timers.delete(id)
          timer.callback()
        }
      }
    },
    get size() { return timers.size },
  }
}

async function fixture(t, kind = 'personal', role = 'user') {
  const clock = createClock()
  const mounted = []
  const unmounted = []
  const stops = []
  const listeners = new Map()
  const saved = []
  const copied = []
  let authentications = 0
  let post = async () => ({ errorNo: 0, data: { codes: ['TEST-ONLY-CODE'] } })
  const store = reactive({
    role,
    userInfos: { account: 'fixture', securitySetupRequired: true },
    securityActiveSection: 'recovery',
    hasCapability: () => false,
    refresh: async () => {},
  })
  const props = reactive({ modelValue: true, user: { ID: 1, RecoveryCodeConfigured: true } })
  const document = {
    hidden: false,
    addEventListener(name, callback) { listeners.set(name, callback) },
    removeEventListener(name) { listeners.delete(name) },
  }
  const environment = {
    computed, reactive, ref,
    watch(...args) {
      const stop = watch(...args)
      stops.push(stop)
      return stop
    },
    onMounted(callback) { mounted.push(callback) },
    onUnmounted(callback) { unmounted.push(callback) },
    http: {
      get: async () => ({ errorNo: 0, data: { configured: true } }),
      post: (...args) => post(...args),
    },
    useGlobalStatusStore: () => store,
    webAuthnSupported: () => false,
    lang: {},
    ElNotification: () => {},
    ElMessage: { success() {} },
    ElMessageBox: { confirm: async () => {} },
    confirmRecentAuthentication: async () => { authentications += 1 },
    requestWithRecentAuthentication: action => action(),
    isRecentAuthenticationCancelled: () => false,
    downloadRecoveryCodes: codes => saved.push([...codes]),
    RECOVERY_CODE_DISPLAY_MS,
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clearTimeout,
    clearInterval: () => {},
    document,
    navigator: { clipboard: { writeText: async value => copied.push(value) } },
    props,
    emit: (name, value) => { if (name === 'update:modelValue') props.modelValue = value },
  }
  const relative = kind === 'personal'
    ? 'SecuritySettings/SecuritySettings.js'
    : 'UserManagement/RecoveryCodeDialog/RecoveryCodeDialog.js'
  const source = readFileSync(new URL('../../frontend/src/views/components/' + relative, import.meta.url), 'utf8')
  const code = source.replace(/^import[\s\S]*?from .*$/gm, '').replace(/^export /gm, '')
  const entry = kind === 'personal' ? 'usePage()' : 'useRecoveryCodeDialog(props, emit)'
  const page = compileFunction(code + '\nreturn ' + entry, Object.keys(environment))(...Object.values(environment))
  let disposed = false
  const dispose = () => {
    if (disposed) return
    disposed = true
    unmounted.forEach(callback => callback())
    stops.forEach(stop => stop())
  }
  t.after(dispose)
  for (const callback of mounted) await callback()
  // 管理弹窗的立即侦听会异步读取状态，等待该次微任务完成。
  await new Promise(resolve => setImmediate(resolve))
  return {
    page, clock, store, props, saved, copied, dispose,
    codes: kind === 'personal' ? page.generatedCodes : page.codes,
    generate: kind === 'personal' ? page.generateRecoveryCodes : page.generateCodes,
    reveal: kind === 'personal' ? page.revealRecoveryCodes : page.revealCodes,
    exportCodes: kind === 'personal' ? page.saveRecoveryCodes : page.copyCodes,
    get authentications() { return authentications },
    setPost(callback) { post = callback },
    hidePage() {
      document.hidden = true
      listeners.get('visibilitychange')?.()
    },
  }
}

for (const role of ['super_admin', 'admin', 'user']) {
  for (const action of ['generate', 'reveal']) {
    test('个人安全码完整显示六十秒后清除且不能继续保存：' + role + '/' + action, async t => {
      const f = await fixture(t, 'personal', role)
      await f[action]()
      assert.equal(f.codes.value.length, 1)
      f.clock.advance(59999)
      assert.equal(f.codes.value.length, 1)
      await f.exportCodes()
      assert.equal(f.saved.length, 1)
      f.clock.advance(1)
      assert.equal(f.codes.value.length, 0)
      await f.exportCodes()
      assert.equal(f.saved.length, 1)
      assert.equal(f.clock.size, 0)
      assert.equal(f.page.recoveryConfigured.value, true)
    })
  }
}

for (const action of ['generate', 'reveal']) {
  test('管理员弹窗在六十秒边界清除安全码与复制来源：' + action, async t => {
    const f = await fixture(t, 'managed')
    await f[action]()
    assert.equal(f.codes.value.length, 1)
    f.clock.advance(59999)
    assert.equal(f.codes.value.length, 1)
    await f.exportCodes()
    assert.equal(f.copied.length, 1)
    f.clock.advance(1)
    assert.equal(f.codes.value.length, 0)
    assert.equal(f.page.revealed.value, false)
    await f.exportCodes()
    assert.equal(f.copied.length, 1)
    assert.equal(f.page.configured.value, true)
  })
}

for (const kind of ['personal', 'managed']) {
  test('重新验证查看安全码会重启计时且旧计时不会提前清除：' + kind, async t => {
    const f = await fixture(t, kind)
    await f.reveal()
    f.clock.advance(30000)
    await f.reveal()
    assert.equal(f.authentications, 2)
    assert.equal(f.clock.size, 1)
    f.clock.advance(30000)
    assert.equal(f.codes.value.length, 1)
    f.clock.advance(29999)
    assert.equal(f.codes.value.length, 1)
    f.clock.advance(1)
    assert.equal(f.codes.value.length, 0)
  })

  for (const action of ['hidePage', 'dispose']) {
    test('页面隐藏或卸载立即清理安全码和计时：' + kind + '/' + action, async t => {
      const f = await fixture(t, kind)
      await f.reveal()
      f[action]()
      assert.equal(f.codes.value.length, 0)
      assert.equal(f.clock.size, 0)
      await f.exportCodes()
      assert.equal(f.saved.length + f.copied.length, 0)
    })

    test('页面离开后晚到的响应不能重新显示安全码：' + kind + '/' + action, async t => {
      const f = await fixture(t, kind)
      let finish
      f.setPost(() => new Promise(resolve => { finish = resolve }))
      const pending = f.reveal()
      await new Promise(resolve => setImmediate(resolve))
      assert.equal(typeof finish, 'function')
      f[action]()
      finish({ errorNo: 0, data: { codes: ['LATE-TEST-CODE'] } })
      await pending
      assert.equal(f.codes.value.length, 0)
      assert.equal(f.clock.size, 0)
    })
  }
}

test('切换个人设置分区立即清除安全码', async t => {
  const f = await fixture(t)
  await f.reveal()
  f.store.securityActiveSection = 'external-email'
  await Promise.resolve()
  assert.equal(f.codes.value.length, 0)
  assert.equal(f.clock.size, 0)
})

test('管理员关闭弹窗或切换用户立即清除安全码', async t => {
  const f = await fixture(t, 'managed')
  await f.reveal()
  f.props.user = { ID: 2, RecoveryCodeConfigured: true }
  assert.equal(f.codes.value.length, 0)
  assert.equal(f.clock.size, 0)
  await new Promise(resolve => setImmediate(resolve))
  await f.reveal()
  assert.equal(f.codes.value.length, 1)
  f.page.close()
  assert.equal(f.props.modelValue, false)
  assert.equal(f.codes.value.length, 0)
  assert.equal(f.clock.size, 0)
})
