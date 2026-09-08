import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'

const [workspace, evidence] = process.argv.slice(2)
if (!workspace || !evidence) throw new Error('请指定项目根目录和本次验证目录')
const digest = data => createHash('sha256').update(data).digest('hex')
const read = filename => readFileSync(filename, 'utf8')
const names = ['config.json', 'smtp.json', 'email.json', 'ssl.json', 'plugins.json']

// 只忽略字符串外的 JSON 空白，数字精度、中文、转义及字段顺序都必须保持。
function compact(raw) {
  JSON.parse(raw)
  let quoted = false
  let escaped = false
  let result = ''
  for (const character of raw) {
    if (quoted) {
      result += character
      if (escaped) escaped = false
      else if (character === '\\') escaped = true
      else if (character === '"') quoted = false
    } else if (character === '"') {
      quoted = true
      result += character
    } else if (!' \t\r\n'.includes(character)) result += character
  }
  return result
}

const report = { checkedAt: new Date().toISOString(), files: [] }
for (const [relative, backup] of [['config', 'root-config'], ['yunx-test/config', 'runtime-config']]) {
  const directory = path.join(workspace, relative)
  assert.deepEqual(readdirSync(directory).sort(), [...names].sort(), '配置目录出现额外文件')
  const oldRoot = JSON.parse(read(path.join(evidence, backup, 'config.json')))
  const newRoot = JSON.parse(read(path.join(directory, 'config.json')))
  for (const name of names) {
    const oldRaw = read(path.join(evidence, backup, name))
    const newRaw = read(path.join(directory, name))
    assert(!newRaw.startsWith('\ufeff'), '配置不能含 BOM')
    assert(newRaw.includes('\n  "'), '配置未按两空格缩进分行')
    let expected = compact(oldRaw)
    if (name === 'config.json') {
      for (const [section, reference] of Object.entries(oldRoot.sections)) {
        expected = expected.replace(`"sha256":"${reference.sha256}"`, `"sha256":"${newRoot.sections[section].sha256}"`)
      }
    } else {
      const section = name.slice(0, -5)
      assert.equal(newRoot.sections[section].sha256, digest(Buffer.from(newRaw)), '新文件摘要不匹配')
    }
    // 不输出配置原文或断言两侧内容，避免凭据进入测试日志。
    assert(expected === compact(newRaw), `${relative}/${name} 内容发生变化`)
    report.files.push({ file: `${relative}/${name}`, lines: newRaw.split('\n').length, valuesUnchanged: true })
  }
}
const before = JSON.parse(read(path.join(evidence, 'before.json')).replace(/^\ufeff/, ''))
const audit = readFileSync(path.join(workspace, 'yunx-test/log/config-audit-v2.jsonl'))
report.auditUnchanged = digest(audit).toUpperCase() === before.auditSHA256
assert(report.auditUnchanged, '审计原始文件发生变化')
writeFileSync(path.join(evidence, 'verification.json'), JSON.stringify(report, null, 2) + '\n')
console.log('验证通过：10 个 JSON 仅变更空白和对应摘要；中文、密文、数字、版本及审计原字节保持不变。')
for (const file of report.files) console.log(`${file.file}：${file.lines} 行`)
