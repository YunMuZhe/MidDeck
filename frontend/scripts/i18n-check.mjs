// Fails if a UI string passed to t('...') has no Chinese translation in src/zh.ts.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const files = []
const walk = (d) => readdirSync(d).forEach((f) => { const p = join(d, f); statSync(p).isDirectory() ? walk(p) : /\.tsx?$/.test(p) && files.push(p) })
walk('src')

const used = new Set()
for (const f of files) {
  const s = readFileSync(f, 'utf8')
  for (const m of s.matchAll(/\bt\(\s*'((?:[^'\\]|\\.)*)'/g)) used.add(m[1].replace(/\\'/g, "'"))
  for (const m of s.matchAll(/\bt\(\s*"((?:[^"\\]|\\.)*)"/g)) used.add(m[1].replace(/\\"/g, '"'))
}
const zh = readFileSync('src/zh.ts', 'utf8')
const missing = [...used].filter((k) => !zh.includes(JSON.stringify(k).slice(1, -1)) && !zh.includes(`'${k.replace(/'/g, "\\'")}'`) && !new RegExp(`^\\s*${k.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}:`, 'm').test(zh))
if (missing.length) {
  console.error(`Missing Chinese translations (${missing.length}):\n` + missing.map((k) => '  ' + k).join('\n'))
  process.exit(1)
}
console.log(`i18n ok: ${used.size} strings translated`)
