import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  baseNameOf,
  buildDownloadPayload,
  collectCookies,
  describeHostError,
  extensionOf,
  hostOf,
  mimeOf,
  sendToHost,
  shouldCapture,
  type CaptureSettings,
} from './types.ts'

function item(overrides: Partial<chrome.downloads.DownloadItem> = {}): chrome.downloads.DownloadItem {
  return {
    id: 1,
    url: 'https://example.com/file.iso',
    filename: '/home/user/Downloads/file.iso',
    referrer: '',
    mime: 'application/octet-stream',
    fileSize: 0,
    totalBytes: 0,
    ...overrides,
  } as chrome.downloads.DownloadItem
}

function settings(overrides: Partial<CaptureSettings> = {}): CaptureSettings {
  return {
    enabled: true,
    minSize: 0,
    allowHosts: [],
    denyHosts: [],
    ...overrides,
  }
}

test('hostOf returns the hostname and null for garbage', () => {
  assert.equal(hostOf('https://cdn.example.com/a/b.bin'), 'cdn.example.com')
  assert.equal(hostOf('http://127.0.0.1:8099/notes.txt'), '127.0.0.1')
  assert.equal(hostOf('not a url'), null)
})

test('extensionOf returns the lowercased final suffix', () => {
  assert.equal(extensionOf('https://example.com/FILE.ISO'), 'iso')
  assert.equal(extensionOf('https://example.com/a.b/c.tar.gz'), 'gz')
  assert.equal(extensionOf('https://example.com/a/b'), '')
  assert.equal(extensionOf('https://example.com/a/.hidden'), '')
  assert.equal(extensionOf('https://example.com/a/trailing.'), '')
  assert.equal(extensionOf('nonsense'), '')
})

test('mimeOf lowercases and tolerates a missing mime', () => {
  assert.equal(mimeOf(item({ mime: 'Application/ISO' })), 'application/iso')
  assert.equal(mimeOf(item({ mime: '' })), '')
})

test('baseNameOf keeps the final path segment only', () => {
  assert.equal(baseNameOf('/home/user/Downloads/file.iso'), 'file.iso')
  assert.equal(baseNameOf('C:\\Users\\me\\file.iso'), 'file.iso')
  assert.equal(baseNameOf('file.iso'), 'file.iso')
  assert.equal(baseNameOf('   '), '')
  assert.equal(baseNameOf('/home/user/'), '')
})

test('shouldCapture rejects downloads under the minimum size', () => {
  assert.equal(shouldCapture(item({ fileSize: 1024 }), settings({ minSize: 2048 })).capture, false)
  assert.equal(shouldCapture(item({ fileSize: 4096 }), settings({ minSize: 2048 })).capture, true)
  // An unknown size must not silently drop a download.
  assert.equal(shouldCapture(item({ fileSize: 0 }), settings({ minSize: 2048 })).capture, true)
})

test('shouldCapture honours deny and allow lists', () => {
  const url = 'https://cdn.example.com/file.bin'
  assert.equal(shouldCapture(item({ url }), settings({ denyHosts: ['cdn.example.com'] })).capture, false)
  assert.equal(shouldCapture(item({ url }), settings({ allowHosts: ['example.com'] })).capture, false)
  assert.equal(shouldCapture(item({ url }), settings({ allowHosts: ['cdn.example.com'] })).capture, true)
  // An empty allow list means every host is allowed.
  assert.equal(shouldCapture(item({ url }), settings({ allowHosts: [] })).capture, true)
})

test('shouldCapture explains why it declined', () => {
  assert.equal(
    shouldCapture(item({ fileSize: 1 }), settings({ minSize: 10 })).reason,
    'below min size',
  )
  assert.equal(
    shouldCapture(item({ url: 'https://a.example/x' }), settings({ denyHosts: ['a.example'] })).reason,
    'denied host',
  )
  assert.equal(
    shouldCapture(item({ url: 'https://a.example/x' }), settings({ allowHosts: ['b.example'] })).reason,
    'not in allow list',
  )
})

test('buildDownloadPayload carries the referrer and the file name', () => {
  const payload = buildDownloadPayload(
    item({ referrer: 'https://example.com/page' }),
    [{ name: 'session', value: 'abc' }],
  )

  assert.equal(payload.command, 'add')
  assert.equal(payload.url, 'https://example.com/file.iso')
  assert.equal(payload.referrer, 'https://example.com/page')
  assert.equal(payload.filename, 'file.iso')
  assert.equal(payload.headers['Referer'], 'https://example.com/page')
  assert.equal(payload.headers['User-Agent'], 'dm/0.2.0')
  assert.deepEqual(payload.cookies, [{ name: 'session', value: 'abc' }])
})

test('buildDownloadPayload omits an empty referrer and unknown filename', () => {
  const payload = buildDownloadPayload(item({ referrer: '', filename: '' }))
  assert.equal(payload.referrer, '')
  assert.equal(payload.filename, undefined)
  assert.equal(payload.headers['Referer'], undefined)
  assert.deepEqual(payload.cookies, [])
})

test('collectCookies returns usable cookies and drops nameless ones', async () => {
  const got = await collectCookies('https://example.com/a', async () => [
    { name: 'session', value: 'abc' },
    { name: '', value: 'orphan' },
  ])
  assert.deepEqual(got, [{ name: 'session', value: 'abc' }])
})

test('collectCookies passes the URL through and survives a failure', async () => {
  let seen = ''
  await collectCookies('https://example.com/a?b=1', async (details) => {
    seen = details.url
    return []
  })
  assert.equal(seen, 'https://example.com/a?b=1')

  const failed = await collectCookies('https://example.com/a', async () => {
    throw new Error('permission denied')
  })
  assert.deepEqual(failed, [])
})

test('sendToHost maps a successful reply', async () => {
  const response = await sendToHost({ command: 'add' }, async (host, message) => {
    assert.equal(host, 'io.github.vishwa.dm')
    assert.deepEqual(message, { command: 'add' })
    return { ok: true, id: 'dl1', filename: 'file.iso' }
  })
  assert.deepEqual(response, { ok: true, id: 'dl1', filename: 'file.iso', version: undefined, error: undefined })
})

test('sendToHost forwards a host-side error', async () => {
  const response = await sendToHost({ command: 'add' }, async () => ({
    ok: false,
    error: 'The dm daemon is not running.',
  }))
  assert.equal(response.ok, false)
  assert.equal(response.error, 'The dm daemon is not running.')
})

test('sendToHost rejects an unreadable reply', async () => {
  const response = await sendToHost({ command: 'ping' }, async () => 'not an object')
  assert.equal(response.ok, false)
  assert.match(response.error ?? '', /unreadable reply/)
})

test('sendToHost explains a missing native host', async () => {
  const response = await sendToHost({ command: 'ping' }, async () => {
    throw new Error('Specified native messaging host not found.')
  })
  assert.equal(response.ok, false)
  assert.match(response.error ?? '', /not installed/)
})

test('sendToHost reports other transport failures verbatim', async () => {
  const response = await sendToHost({ command: 'ping' }, async () => {
    throw new Error('Host process exited')
  })
  assert.equal(response.ok, false)
  assert.match(response.error ?? '', /Could not reach the DM native host: Host process exited/)
})

test('describeHostError handles non-Error throws', () => {
  assert.match(describeHostError('native host not registered'), /not installed/)
  assert.match(describeHostError(undefined), /Could not reach the DM native host: undefined/)
})
