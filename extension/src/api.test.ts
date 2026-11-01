import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  daemonBaseUrl,
  isDaemonUrl,
  shouldCapture,
  buildDownloadPayload,
  sendToDaemon,
  extensionOf,
  hostOf,
  mimeOf,
} from './types.ts'

test('daemonBaseUrl', () => {
  assert.strictEqual(daemonBaseUrl({ port: 8080 }), 'http://127.0.0.1:8080')
  assert.strictEqual(daemonBaseUrl({ port: 0 }), 'http://127.0.0.1:0')
})

test('isDaemonUrl', () => {
  const settings = { port: 8080, token: 'x', minSize: 0, allowHosts: [], denyHosts: [] }
  assert.ok(isDaemonUrl('http://127.0.0.1:8080/api/downloads', settings))
  assert.ok(isDaemonUrl('http://127.0.0.1:8080/', settings))
  assert.ok(!isDaemonUrl('http://127.0.0.1:9090/', settings))
  assert.ok(!isDaemonUrl('https://example.com/', settings))
  assert.ok(!isDaemonUrl('http://example.com/', { port: 0, token: '', minSize: 0, allowHosts: [], denyHosts: [] }))
})

test('shouldCapture below min size', () => {
  const settings = { port: 8080, token: 'x', minSize: 1000, allowHosts: [], denyHosts: [] }
  const small = { url: 'https://example.com/tiny.bin', mime: 'application/octet-stream', fileSize: 100, totalBytes: 100 } as chrome.downloads.DownloadItem
  const dec = shouldCapture(small, settings)
  assert.strictEqual(dec.capture, false)
  assert.strictEqual(dec.reason, 'below min size')

  const big = { url: 'https://example.com/big.bin', mime: 'application/octet-stream', fileSize: 5000, totalBytes: 5000 } as chrome.downloads.DownloadItem
  const dec2 = shouldCapture(big, settings)
  assert.strictEqual(dec2.capture, true)
})

test('shouldCapture deny host', () => {
  const settings = { port: 8080, token: 'x', minSize: 0, allowHosts: [], denyHosts: ['evil.com'] }
  const item = { url: 'https://evil.com/file.bin', mime: 'application/octet-stream' } as chrome.downloads.DownloadItem
  assert.strictEqual(shouldCapture(item, settings).capture, false)
  assert.strictEqual(shouldCapture(item, settings).reason, 'denied host')

  const ok = { url: 'https://good.com/file.bin', mime: 'application/octet-stream' } as chrome.downloads.DownloadItem
  assert.strictEqual(shouldCapture(ok, settings).capture, true)
})

test('shouldCapture allow list', () => {
  const settings = { port: 8080, token: 'x', minSize: 0, allowHosts: ['example.com'], denyHosts: [] }
  const allowed = { url: 'https://example.com/file.bin', mime: 'application/octet-stream' } as chrome.downloads.DownloadItem
  assert.strictEqual(shouldCapture(allowed, settings).capture, true)

  const notAllowed = { url: 'https://other.com/file.bin', mime: 'application/octet-stream' } as chrome.downloads.DownloadItem
  assert.strictEqual(shouldCapture(notAllowed, settings).capture, false)
  assert.strictEqual(shouldCapture(notAllowed, settings).reason, 'not in allow list')
})

test('shouldCapture skips daemon url', () => {
  const settings = { port: 8080, token: 'x', minSize: 0, allowHosts: [], denyHosts: [] }
  const item = { url: 'http://127.0.0.1:8080/api/downloads', mime: 'application/octet-stream' } as chrome.downloads.DownloadItem
  assert.strictEqual(shouldCapture(item, settings).capture, false)
  assert.strictEqual(shouldCapture(item, settings).reason, 'daemon url')
})

test('buildDownloadPayload sets defaults', () => {
  const item = {
    url: 'https://example.com/file.iso',
    referrer: 'https://example.com/page',
    filename: 'file.iso',
    mime: 'application/octet-stream',
  } as chrome.downloads.DownloadItem
  const settings = { port: 8080, token: 'x', minSize: 0, allowHosts: [], denyHosts: [] }
  const req = buildDownloadPayload(item, settings)
  assert.strictEqual(req.url, 'https://example.com/file.iso')
  assert.strictEqual(req.referrer, 'https://example.com/page')
  assert.strictEqual(req.filename, 'file.iso')
  assert.strictEqual(req.headers['User-Agent'], 'dm/0.1.0-dev')
  assert.strictEqual(req.headers['Referer'], 'https://example.com/page')
  assert.deepStrictEqual(req.cookies, [])
})

test('extensionOf', () => {
  assert.strictEqual(extensionOf('https://example.com/file.iso'), 'iso')
  assert.strictEqual(extensionOf('https://example.com/dir/file.TAR.GZ'), 'gz')
  assert.strictEqual(extensionOf('https://example.com/noext'), '')
  assert.strictEqual(extensionOf('https://example.com/'), '')
})

test('hostOf', () => {
  assert.strictEqual(hostOf('https://example.com/path'), 'example.com')
  assert.strictEqual(hostOf('not a url'), null)
})

test('mimeOf', () => {
  assert.strictEqual(mimeOf({ mime: 'Application/Json' } as chrome.downloads.DownloadItem), 'application/json')
  assert.strictEqual(mimeOf({} as chrome.downloads.DownloadItem), '')
})

test('sendToDaemon forwards token and origin', async () => {
  const captured: { url: string; init: RequestInit }[] = []
  const fakeFetch = async (url: string, init: RequestInit): Promise<Response> => {
    captured.push({ url, init })
    return new Response('ok', { status: 200 })
  }

  const settings = { port: 9090, token: 'secret-token', minSize: 0, allowHosts: [], denyHosts: [] }
  const res = await sendToDaemon(settings, '/downloads', { url: 'https://example.com/f' }, fakeFetch)
  assert.strictEqual(res.status, 200)
  assert.strictEqual(captured.length, 1)
  assert.strictEqual(captured[0].url, 'http://127.0.0.1:9090/api/downloads')
  assert.strictEqual(captured[0].init.method, 'POST')
  assert.strictEqual(captured[0].init.headers?.Authorization, 'Bearer secret-token')
  assert.strictEqual(captured[0].init.headers?.Origin, 'http://127.0.0.1:9090')
})
