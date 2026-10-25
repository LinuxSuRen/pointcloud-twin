import { describe, expect, it } from 'vitest'
import { hashBytes } from './sha256.js'

// 与 WebCrypto 对拍（Node 20+ 提供 webcrypto）
async function refDigest(bytes) {
  const buf = await crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

describe('Sha256Stream', () => {
  it.each([
    ['空输入', ''],
    ['经典向量 abc', 'abc'],
    ['55 字节（块边界前）', 'a'.repeat(55)],
    ['56 字节（触发双块填充）', 'a'.repeat(56)],
    ['63 字节', 'a'.repeat(63)],
    ['64 字节（整块）', 'a'.repeat(64)],
    ['65 字节', 'a'.repeat(65)],
    ['1000 字节随机', null],
  ])('%s', async (_name, content) => {
    let bytes
    if (content === null) {
      bytes = new Uint8Array(1000)
      crypto.getRandomValues(bytes)
    } else {
      bytes = new TextEncoder().encode(content)
    }
    expect(hashBytes(bytes)).toBe(await refDigest(bytes))
  })

  it('增量喂数据与一次性喂等价', async () => {
    const bytes = new Uint8Array(500)
    crypto.getRandomValues(bytes)
    const once = hashBytes(bytes)
    const { Sha256Stream } = await import('./sha256.js')
    const s = new Sha256Stream()
    s.update(bytes.subarray(0, 7))
    s.update(bytes.subarray(7, 70))
    s.update(bytes.subarray(70))
    expect(s.digest()).toBe(once)
  })
})
