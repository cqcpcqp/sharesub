import { readdirSync, readFileSync } from 'node:fs'
import { extname, join } from 'node:path'
import { gzipSync } from 'node:zlib'

const assetsDirectory = new URL('../dist/assets/', import.meta.url)
const budgets = {
  '.js': { raw: 700 * 1024, gzip: 220 * 1024 },
  '.css': { raw: 100 * 1024, gzip: 30 * 1024 },
}

let failed = false
for (const name of readdirSync(assetsDirectory)) {
  const budget = budgets[extname(name)]
  if (!budget) continue
  const contents = readFileSync(join(assetsDirectory.pathname, name))
  const gzipBytes = gzipSync(contents).byteLength
  if (contents.byteLength > budget.raw || gzipBytes > budget.gzip) {
    console.error(`bundle gate: ${name} is ${contents.byteLength} bytes raw / ${gzipBytes} bytes gzip`)
    failed = true
  }
}

if (failed) process.exit(1)
