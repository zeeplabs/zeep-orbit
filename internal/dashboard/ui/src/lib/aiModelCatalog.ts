// aiModelCatalog — the models offered as presets for the OpenAI and Anthropic
// providers, with the providers' published list prices as a reference. It is a
// curated list, reviewed by hand (CATALOG_REVIEWED_AT), not a mirror of either
// catalog: the "custom" option keeps any other model usable.
//
// The prices are a suggestion only (Orbit ships no prices and fetches none): the
// price card offers them to fill its form when the installation currency is USD,
// and the superadmin saves them. Sources: developers.openai.com/api/docs/pricing
// (Standard tier, short context) and platform.claude.com/docs/en/about-claude/pricing.

import type { BuilderProviderKind } from '@/lib/api'

// CATALOG_REVIEWED_AT is the day the models and prices below were checked. Local
// noon, not a bare date: "2026-11-13" parses as UTC midnight and renders as the
// day before west of Greenwich.
export const CATALOG_REVIEWED_AT = '2026-11-13T12:00:00'

// CATALOG_PRICE_CURRENCY is the currency the providers publish prices in.
export const CATALOG_PRICE_CURRENCY = 'USD'

// ReferencePrice is per million tokens, as decimal strings. A null optional
// price means the provider bills no such class (it falls back to the input
// price in the ledger, which is never used for it).
export interface ReferencePrice {
  input: string
  cachedInput: string | null
  cacheWrite: string | null
  output: string
}

export interface CatalogModel {
  id: string
  // price is null when one row cannot represent the provider's price.
  price: ReferencePrice | null
}

const openai = (id: string, input: string, cachedInput: string, output: string): CatalogModel => ({
  id,
  price: { input, cachedInput, cacheWrite: null, output },
})

// Anthropic bills cache writes; the 5-minute write is the default cache TTL.
const anthropic = (id: string, input: string, cacheWrite: string, cachedInput: string, output: string): CatalogModel => ({
  id,
  price: { input, cachedInput, cacheWrite, output },
})

// OpenAI prices are the short-context ones: gpt-5.5 and gpt-5.4 cost more above
// 272K input tokens, which the builder does not reach (compaction runs well below).
export const OPENAI_MODELS: readonly CatalogModel[] = [
  openai('gpt-6-astra', '10', '1', '50'),
  openai('gpt-6.1-sol', '2', '0.1', '10'),
  openai('gpt-6.1-terra', '1', '0.05', '5'),
  openai('gpt-6-luna', '0.1', '0.01', '0.5'),
  openai('gpt-5.5', '5', '0.5', '30'),
  openai('gpt-5.4', '2.5', '0.25', '15'),
  openai('gpt-5.4-mini', '0.75', '0.075', '4.5'),
  openai('gpt-5.4-nano', '0.2', '0.02', '1.25'),
  openai('gpt-5-mini', '0.25', '0.025', '2'),
  openai('gpt-4.1', '2', '0.5', '8'),
  openai('gpt-4.1-mini', '0.4', '0.1', '1.6'),
  openai('gpt-4o', '2.5', '1.25', '10'),
  openai('gpt-4o-mini', '0.15', '0.075', '0.6'),
]

export const ANTHROPIC_MODELS: readonly CatalogModel[] = [
  anthropic('claude-fable-5-2', '8', '10', '0.2', '40'),
  anthropic('claude-opus-5-5', '4', '5', '0.2', '20'),
  anthropic('claude-sonnet-5-5', '2', '2.5', '0.1', '10'),
  // Haiku 5.5 costs five times more when the prompt is over 100,000 tokens,
  // which a long builder session reaches: one price row would undercount it.
  { id: 'claude-haiku-5-5', price: null },
  anthropic('claude-fable-5-1', '10', '12.5', '0.25', '50'),
  anthropic('claude-opus-5', '5', '6.25', '0.5', '25'),
  anthropic('claude-sonnet-5', '2', '2.5', '0.2', '10'),
  anthropic('claude-sonnet-4-6', '3', '3.75', '0.3', '15'),
  anthropic('claude-haiku-4-5', '1', '1.25', '0.1', '5'),
]

// catalogModels is the preset list of a provider; openai_compatible has none.
export function catalogModels(provider: BuilderProviderKind): readonly CatalogModel[] {
  if (provider === 'openai') return OPENAI_MODELS
  if (provider === 'anthropic') return ANTHROPIC_MODELS
  return []
}

export function isCatalogModel(provider: BuilderProviderKind, model: string): boolean {
  return catalogModels(provider).some((m) => m.id === model)
}

// referencePrice is the published price of a catalog model, or null.
export function referencePrice(provider: BuilderProviderKind, model: string): ReferencePrice | null {
  return catalogModels(provider).find((m) => m.id === model)?.price ?? null
}
