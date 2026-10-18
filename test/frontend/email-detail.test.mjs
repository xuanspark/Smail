import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compileFunction } from 'node:vm'
import { computed, reactive, ref, watch } from './vue.mjs'

const source = readFileSync(new URL('../../frontend/src/views/EmailDetail/EmailDetail.js', import.meta.url), 'utf8')
const createPage = compileFunction(
  source.replace(/^import .*$/gm, '').replace('export function usePage', 'function usePage').replace(/export \{[^}]+\}/g, '') + '\nreturn usePage()',
  ['computed', 'ref', 'watch', 'onBeforeUnmount', 'useRoute', 'useRouter', 'useGroupStore', 'http', 'ElMessage', 'ElMessageBox', 'lang', 'Document', 'Back', 'Delete', 'Download', 'Operation']
)
const settle = () => new Promise(resolve => setImmediate(resolve))
const email = id => ({errorNo: 0, data: {id, subject: `mail-${id}`, to: '[{"EmailAddress":"test@example.com"}]', cc: '[]', attachments: []}})

function fixture(post, id = '15') {
  const route = reactive({params: {id}})
  const requests = []
  const navigations = []
  let unmount
  const page = createPage(computed, ref, watch, callback => { unmount = callback }, () => route,
    () => ({push: target => navigations.push(target)}), () => ({tag: ''}),
    {post: async (...args) => { requests.push(args); return post(...args) }}, {success: () => {}, error: () => {}, warning: () => {}, info: () => {}}, {}, {}, {}, {}, {}, {})
  return {page, route, requests, navigations, unmount: () => unmount()}
}

test('邮件业务错误不渲染空数据，允许重试后读取正文与附件', async () => {
  let response = {errorNo: 500, errorMsg: '读取邮件失败', data: ''}
  const {page, requests} = fixture(async () => response)
  assert.equal(page.loading.value, true)
  await settle()
  assert.equal(page.loading.value, false)
  assert.equal(page.loadError.value, '读取邮件失败')
  assert.equal(page.detailData.value.id, undefined)
  page.handleDelete()
  assert.equal(requests.length, 1)
  response = email(15)
  response.data.attachments = [{Index: 0, Filename: '中文附件.txt'}]
  await page.loadDetail()
  assert.equal(page.loadError.value, '')
  assert.equal(page.detailData.value.subject, 'mail-15')
  assert.equal(page.detailData.value.attachments[0].Filename, '中文附件.txt')
  assert.equal(page.tos.value[0].EmailAddress, 'test@example.com')
})

test('网络失败提供中文错误，返回操作明确进入邮件列表', async () => {
  const {page, navigations} = fixture(async () => { throw new Error('Network Error') })
  await settle()
  assert.equal(page.loading.value, false)
  assert.equal(page.loadError.value, '网络连接失败，请稍后重试')
  page.backToList()
  assert.equal(navigations[0].name, 'list')
})

test('无效编号不发请求，空响应或编号不匹配不当作成功', async () => {
  for (const id of ['abc', '15abc', '-1', '0', '9007199254740992']) {
    const {page, requests} = fixture(async () => email(15), id)
    await settle()
    assert.equal(requests.length, 0)
    assert.equal(page.loadError.value, '邮件地址无效，请返回邮件列表')
  }
  for (const data of [null, '', [], {}, {id: 14}]) {
    const {page} = fixture(async () => ({errorNo: 0, data}))
    await settle()
    assert.equal(page.loadError.value, '邮件不可用，请返回邮件列表')
    assert.equal(page.detailData.value.id, undefined)
  }
})

test('异常地址和附件字段不导致页面异常，切换邮件清除旧收件人', async () => {
  const {page, route} = fixture(async (_, {id}) => ({errorNo: 0, data: {id, to: id === 15 ? '[{"EmailAddress":"old@example.com"}]' : 'null', cc: id === 15 ? '[{"EmailAddress":"copy@example.com"}]' : '{', attachments: 'bad'}}))
  await settle()
  assert.equal(page.showCC.value, true)
  assert.equal(page.detailData.value.attachments.length, 0)
  route.params.id = '16'
  await settle()
  assert.equal(page.detailData.value.id, 16)
  assert.equal(page.tos.value.length, 0)
  assert.equal(page.ccs.value.length, 0)
  assert.equal(page.showCC.value, false)
})

test('仅发件副本展示密送名单，切换到收件副本立即清除', async () => {
  const addresses = '[{"Name":"","EmailAddress":"hidden@example.com"}]'
  const {page, route} = fixture(async (_, {id}) => ({
    errorNo: 0,
    data: {...email(id).data, type: id === 15 ? 1 : 0, bcc: addresses}
  }))
  await settle()
  assert.equal(page.showBCC.value, true)
  assert.equal(page.bccs.value[0].EmailAddress, 'hidden@example.com')
  route.params.id = '16'
  await settle()
  assert.equal(page.showBCC.value, false)
  assert.deepEqual(page.bccs.value, [])
})

test('未知邮件类型和无效密送字段不展示名单，加载失败不残留旧名单', async () => {
  for (const data of [
    {bcc: '[{"EmailAddress":"hidden@example.com"}]'},
    {type: 1, bcc: '{'},
    {type: 1, bcc: 'null'},
    {type: 1, bcc: '[]'}
  ]) {
    const {page} = fixture(async () => ({errorNo: 0, data: {...email(15).data, ...data}}))
    await settle()
    assert.equal(page.showBCC.value, false)
    assert.deepEqual(page.bccs.value, [])
  }
  const {page, route} = fixture(async (_, {id}) => id === 15
    ? {errorNo: 0, data: {...email(id).data, type: 1, bcc: '[{"EmailAddress":"hidden@example.com"}]'}}
    : {errorNo: 403, errorMsg: '无权查看邮件'})
  await settle()
  assert.equal(page.showBCC.value, true)
  route.params.id = '16'
  await settle()
  assert.equal(page.showBCC.value, false)
  assert.deepEqual(page.bccs.value, [])
})

test('切换邮件后旧请求成功或失败均不能覆盖新邮件', async () => {
  for (const staleFailure of [false, true]) {
    let finish
    let fail
    const {page, route} = fixture((_, {id}) => id === 15 ? new Promise((resolve, reject) => { finish = resolve; fail = reject }) : Promise.resolve(email(id)))
    route.params.id = '16'
    await settle()
    assert.equal(page.detailData.value.id, 16)
    if (staleFailure) fail(new Error('Network Error'))
    else finish(email(15))
    await settle()
    assert.equal(page.detailData.value.id, 16)
    assert.equal(page.loadError.value, '')
    assert.equal(page.loading.value, false)
  }
})

test('离开详情后未完成请求不再写入状态', async () => {
  let finish
  const {page, unmount} = fixture(() => new Promise(resolve => { finish = resolve }))
  unmount()
  finish(email(15))
  await settle()
  assert.equal(page.detailData.value.id, undefined)
})

test('一键翻译与查看原文切换，支持缓存翻译结果', async () => {
  let translateCalls = 0
  const postHandler = async (url, data) => {
    if (url === '/api/v1/email/detail') {
      return {
        errorNo: 0,
        data: {
          id: 15,
          subject: 'Hello World',
          text: 'This is an English email.',
          type: 0,
          to: '[]',
          cc: '[]',
          attachments: []
        }
      }
    }
    if (url === '/api/v1/email/translate') {
      translateCalls++
      return {
        errorNo: 0,
        data: {
          id: 15,
          subject_translated: '你好世界',
          content_translated: '这是一封英文邮件。',
          detected_lang: 'en'
        }
      }
    }
    return { errorNo: 500, errorMsg: '未知请求' }
  }

  const { page } = fixture(postHandler, '15')
  await settle()

  assert.equal(page.displaySubject.value, 'Hello World')
  assert.equal(page.displayContent.value, 'This is an English email.')
  assert.equal(page.isTranslated.value, false)

  // 第一次触发翻译
  await page.toggleTranslate()
  assert.equal(translateCalls, 1)
  assert.equal(page.isTranslated.value, true)
  assert.equal(page.displaySubject.value, '你好世界')
  assert.equal(page.displayContent.value, '这是一封英文邮件。')
  assert.equal(page.detectedLang.value, 'en')

  // 点击查看原文
  await page.toggleTranslate()
  assert.equal(translateCalls, 1)
  assert.equal(page.isTranslated.value, false)
  assert.equal(page.displaySubject.value, 'Hello World')
  assert.equal(page.displayContent.value, 'This is an English email.')

  // 再次切换到译文（应命中本地缓存，不重复发请求）
  await page.toggleTranslate()
  assert.equal(translateCalls, 1)
  assert.equal(page.isTranslated.value, true)
  assert.equal(page.displaySubject.value, '你好世界')
  assert.equal(page.displayContent.value, '这是一封英文邮件。')
})
