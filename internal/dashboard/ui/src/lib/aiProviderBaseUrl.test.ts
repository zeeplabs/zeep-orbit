import { describe, expect, it } from 'vitest'

import { aiProviderBaseUrlChanged } from './aiProviderBaseUrl'

describe('aiProviderBaseUrlChanged', () => {
  it('ignores spaces and trailing slashes, like the server normalization', () => {
    expect(aiProviderBaseUrlChanged(' https://a.example/v1/ ', 'https://a.example/v1')).toBe(false)
    expect(aiProviderBaseUrlChanged('https://a.example/v1', 'https://a.example/v1')).toBe(false)
  })

  it('reports a different host or path', () => {
    expect(aiProviderBaseUrlChanged('https://b.example/v1', 'https://a.example/v1')).toBe(true)
    expect(aiProviderBaseUrlChanged('https://a.example/v2', 'https://a.example/v1')).toBe(true)
  })

  it('reports a first base URL as a change', () => {
    expect(aiProviderBaseUrlChanged('https://a.example/v1', '')).toBe(true)
  })
})
