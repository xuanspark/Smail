import { execFileSync } from 'node:child_process'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = fileURLToPath(new URL('../../', import.meta.url))
const git = args => execFileSync('git', args, { cwd: root, encoding: 'utf8', windowsHide: true }).trim()
const head = git(['rev-parse', 'HEAD'])
const records = JSON.parse(await readFile(new URL('./reviewed-ranges.json', import.meta.url), 'utf8'))
if (head !== records.baseline) throw new Error('审查基线已改变，需要先复核覆盖记录')
const files = git(['ls-files', '-z']).split('\0').filter(Boolean)
const trackedFiles = new Set(files)
for (const name of Object.keys(records.files)) {
  if (!trackedFiles.has(name)) throw new Error(`审查记录路径不在 Git 清单中，请核对大小写: ${name}`)
}
const directory = path.join(root, 'test-log', `coverage-${new Date().toISOString().replace(/[:.]/g, '-')}`)
await mkdir(directory, { recursive: true })
const rows = []
for (const name of files) {
  const bytes = await readFile(path.join(root, name))
  let text = null
  try { text = new TextDecoder('utf-8', { fatal: true }).decode(bytes) } catch {}
  if (bytes.includes(0)) text = null
  const lines = text === null ? 0 : text.length ? text.split('\n').length - Number(text.endsWith('\n')) : 0
  const extension = path.extname(name).toLowerCase()
  let category = 'configuration'
  if (text === null) category = 'binary'
  else if (/(^|\/)(package-lock\.json|pnpm-lock\.yaml|yarn\.lock|go\.sum)$/.test(name)) category = 'dependency-lock'
  else if (/\.min\.|\/static\/|\/lib\/|\/nuisance\/|\.csv$/.test(name)) category = 'vendor-or-data'
  else if (/\.md$|LICENSE/.test(name)) category = 'documentation'
  else if (/\.(svg|gif|png|jpg|ico|txt)$/.test(name)) category = 'asset-or-data'
  else if (name.startsWith('test/') || /_test\.go$|\.test\./.test(name)) category = 'test'
  else if (['.go', '.js', '.mjs', '.cjs', '.vue', '.css', '.html', '.sh', '.ps1', '.bat', '.ts'].includes(extension)) category = 'source'
  const review = records.files[name]
  const ranges = review?.ranges?.map(([first, last]) => [first, last === 'EOF' ? lines : last]) || []
  const covered = new Set()
  for (const [first, last] of ranges) {
    if (first < 1 || last > lines || first > last) throw new Error(`无效覆盖行范围: ${name}`)
    for (let line = first; line <= last; line++) covered.add(line)
  }
  rows.push({
    path: name, category, lines, sha256: createHash('sha256').update(bytes).digest('hex'),
    status: covered.size === 0 ? 'not-recorded' : covered.size === lines ? 'read-not-accepted' : 'partial-read',
    readLines: covered.size, ranges, evidence: review?.evidence || '', findings: review?.findings || []
  })
}
const summary = {
  baseline: head, generatedAt: new Date().toISOString(), files: rows.length,
  textLines: rows.reduce((sum, row) => sum + row.lines, 0),
  recordedReadLines: rows.reduce((sum, row) => sum + row.readLines, 0),
  recordedFullReads: rows.filter(row => row.status === 'read-not-accepted').length,
  recordedPartialReads: rows.filter(row => row.status === 'partial-read').length,
  notRecorded: rows.filter(row => row.status === 'not-recorded').length,
  categories: Object.fromEntries([...new Set(rows.map(row => row.category))].map(category => [category, {
    files: rows.filter(row => row.category === category).length,
    lines: rows.filter(row => row.category === category).reduce((sum, row) => sum + row.lines, 0)
  }]))
}
await writeFile(path.join(directory, 'inventory.json'), JSON.stringify({ summary, rows }, null, 2) + '\n')
console.log(JSON.stringify({ directory: path.relative(root, directory), ...summary }, null, 2))
