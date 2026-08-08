import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const version = readFileSync(resolve(__dirname, '../VERSION'), 'utf8').trim()
const configuredVersion = process.env.SHARESUB_BUILD_VERSION
if (configuredVersion !== undefined && configuredVersion !== version) {
  throw new Error(`SHARESUB_BUILD_VERSION ${configuredVersion} does not match VERSION ${version}`)
}
const revision = process.env.SHARESUB_BUILD_REVISION
  ?? execFileSync('git', ['rev-parse', 'HEAD'], { cwd: resolve(__dirname, '..'), encoding: 'utf8' }).trim()
if (!/^[0-9a-f]{40}$/.test(revision)) {
  throw new Error(`SHARESUB_BUILD_REVISION must be a full Git SHA, received ${revision}`)
}

export default defineConfig({
  plugins: [vue()],
  define: {
    __SHARESUB_VERSION__: JSON.stringify(version),
    __SHARESUB_REVISION__: JSON.stringify(revision),
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/v1': 'http://127.0.0.1:8080',
      '/responses': 'http://127.0.0.1:8080',
      '/backend-api': 'http://127.0.0.1:8080',
    },
  },
})
