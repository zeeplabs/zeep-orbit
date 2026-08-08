import { countries, globalCellphoneMask, clearMask } from '@zeeptech/toolkit'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const DEFAULT_COUNTRY = 'BR'

const sortedCountries = [...countries].sort((a, b) => {
  if (a.code === DEFAULT_COUNTRY) return -1
  if (b.code === DEFAULT_COUNTRY) return 1
  return a.en.localeCompare(b.en)
})

/**
 * Splits an E.164-ish value (`+{dialCode}{digits}`) into its country code and
 * national digits, matching the longest `dialCode` prefix (some dial codes
 * are prefixes of others, e.g. `+1` vs `+1264`). Falls back to `BR` with the
 * raw digits as the national number when nothing matches (empty value,
 * legacy data with no `+` prefix, or an unrecognized dial code).
 */
function parsePhone(value: string): { country: string; national: string } {
  if (!value.startsWith('+')) {
    return { country: DEFAULT_COUNTRY, national: clearMask(value) }
  }

  let best: { code: string; dialCode: string } | null = null
  for (const c of countries) {
    if (value.startsWith(c.dialCode) && (!best || c.dialCode.length > best.dialCode.length)) {
      best = { code: c.code, dialCode: c.dialCode }
    }
  }

  if (!best) {
    return { country: DEFAULT_COUNTRY, national: clearMask(value) }
  }

  return { country: best.code, national: value.slice(best.dialCode.length).replace(/\D/g, '') }
}

function dialCodeFor(country: string): string {
  return countries.find((c) => c.code === country)?.dialCode ?? ''
}

function maskFor(country: string): string {
  return countries.find((c) => c.code === country)?.mask ?? ''
}

/**
 * Country-select + masked phone input. Owns country selection and masking
 * internally so the parent only ever sees/sends a single E.164-ish string
 * (`+{dialCode}{digits}`), never a raw unmasked free-text value.
 */
export function PhoneInput({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const { country, national } = parsePhone(value)
  const masked = globalCellphoneMask(country, national)

  const setCountry = (nextCountry: string) => {
    onChange(national ? `${dialCodeFor(nextCountry)}${national}` : '')
  }

  const setNational = (nextMasked: string) => {
    const digits = clearMask(nextMasked)
    onChange(digits ? `${dialCodeFor(country)}${digits}` : '')
  }

  return (
    <div className="flex gap-2">
      <Select value={country} onValueChange={setCountry}>
        <SelectTrigger className="w-[150px] shrink-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {sortedCountries.map((c) => (
            <SelectItem key={c.code} value={c.code}>
              {c.flag} {c.en} ({c.dialCode})
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Input
        value={masked}
        onChange={(e) => setNational(e.target.value)}
        placeholder={maskFor(country)}
        className="flex-1"
      />
    </div>
  )
}
