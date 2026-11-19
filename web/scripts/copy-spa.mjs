import { cpSync, mkdirSync, readdirSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distDir = resolve(webRoot, 'dist')
const spaDir = resolve(webRoot, '..', 'gateway', 'spa')

mkdirSync(spaDir, { recursive: true })
rmSync(resolve(spaDir, 'assets'), { force: true, recursive: true })
for (const entry of readdirSync(distDir, { withFileTypes: true })) {
  cpSync(resolve(distDir, entry.name), resolve(spaDir, entry.name), { recursive: true })
}
