import esbuild from 'esbuild'
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'

// One bundle, two self-contained extension directories: Chrome wants a
// service worker, Firefox MV3 still uses event pages with background scripts.
// Start from an empty dist so a removed target or an old layout cannot leave
// files behind that the browser would still load.
await rm('dist', { recursive: true, force: true })

await esbuild.build({
  entryPoints: ['src/background.ts', 'src/popup.ts', 'src/options.ts'],
  bundle: true,
  format: 'iife',
  target: 'es2022',
  outdir: 'dist/build',
  sourcemap: true,
  minify: false,
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
})

const template = JSON.parse(await readFile('manifest.json', 'utf8'))
const staticFiles = ['popup.html', 'options.html', 'popup.css', 'options.css']

for (const target of ['chrome', 'firefox']) {
  const out = `dist/${target}`
  await rm(out, { recursive: true, force: true })
  await mkdir(out, { recursive: true })
  await cp('dist/build', out, { recursive: true })
  for (const file of staticFiles) {
    await cp(`src/${file}`, `${out}/${file}`)
  }

  const manifest = structuredClone(template)
  if (target === 'firefox') {
    manifest.background = { scripts: ['background.js'] }
    manifest.browser_specific_settings = {
      gecko: {
        id: 'dm@vishwa.github.io',
        strict_min_version: '121.0',
      },
    }
  }

  await writeFile(`${out}/manifest.json`, `${JSON.stringify(manifest, null, 2)}\n`)
}

await rm('dist/build', { recursive: true, force: true })
console.log('extension built -> dist/chrome and dist/firefox')
