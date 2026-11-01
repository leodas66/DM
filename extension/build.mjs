import esbuild from 'esbuild'

await esbuild.build({
  entryPoints: ['src/background.ts', 'src/popup.ts', 'src/options.ts'],
  bundle: true,
  format: 'iife',
  target: 'chrome120',
  outdir: 'dist',
  sourcemap: true,
  minify: false,
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
})

console.log('extension built -> dist/')
