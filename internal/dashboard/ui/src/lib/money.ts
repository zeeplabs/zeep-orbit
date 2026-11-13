import i18n from './i18n'

/**
 * Money helpers of the AI Builder cost screens. The API carries money as integer micros
 * (1 unit of currency = 1,000,000 micros) and prices as decimal strings; nothing here goes
 * through a float on the way in, so what the operator types is what is stored.
 */

export const MICROS_PER_UNIT = 1_000_000

const DECIMAL_RE = /^\d{1,12}(\.\d{1,6})?$/

/** True for a non-negative decimal with at most 12 integer digits and 6 decimals ("3", "0.15"). */
export function isDecimalAmount(text: string): boolean {
  return DECIMAL_RE.test(text.trim())
}

/** "12.5" -> 12500000. null when the text is not a valid amount or does not fit a safe integer. */
export function microsFromDecimal(text: string): number | null {
  const value = text.trim()
  if (!DECIMAL_RE.test(value)) return null
  const [whole, frac = ''] = value.split('.')
  const micros = Number(whole) * MICROS_PER_UNIT + Number(frac.padEnd(6, '0'))
  return Number.isSafeInteger(micros) ? micros : null
}

/** 12500000 -> "12.50". Trailing zeros are trimmed, keeping at least two decimals. */
export function decimalFromMicros(micros: number): string {
  const whole = Math.trunc(micros / MICROS_PER_UNIT)
  let frac = String(Math.abs(micros % MICROS_PER_UNIT)).padStart(6, '0').replace(/0+$/, '')
  while (frac.length < 2) frac += '0'
  return `${whole}.${frac}`
}

/**
 * Formats micros in the installation currency, in the UI language. Amounts under one unit keep
 * up to four decimals, because a single model call often costs a fraction of a cent.
 */
export function formatMoney(micros: number, currency: string): string {
  const units = micros / MICROS_PER_UNIT
  const small = Math.abs(units) < 1
  try {
    return new Intl.NumberFormat(i18n.language || 'en', {
      style: 'currency',
      currency,
      minimumFractionDigits: 2,
      maximumFractionDigits: small ? 4 : 2,
    }).format(units)
  } catch {
    // A currency code Intl does not accept: show the number and the code as typed.
    return `${decimalFromMicros(micros)} ${currency}`
  }
}
