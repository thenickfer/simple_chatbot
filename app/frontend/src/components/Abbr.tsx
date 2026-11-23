import type { ReactNode } from 'react'
import {
  getAbbreviationExpansion,
  tokenizeProseAbbreviations,
  type TokenizeAbbreviationsOptions,
} from '../lib/abbreviations.ts'

export interface AbbrProps {
  title: string
  children: ReactNode
}

/**
 * Pure body-prose abbreviation element.
 * Never render `<Abbr>` or `<Abbreviated>` inside heroes, headers, nav, footers,
 * headings (h1-h6), buttons, links, stamps, badges, or monospace metadata labels.
 */
export function Abbr({ title, children }: AbbrProps) {
  const cleanTitle = title.trim()
  if (!cleanTitle) {
    return <>{children}</>
  }
  return <abbr title={cleanTitle}>{children}</abbr>
}

export interface AbbreviatedProps extends TokenizeAbbreviationsOptions {
  text: string
}

export function Abbreviated({ text, firstOccurrenceOnly = false, seen }: AbbreviatedProps) {
  const tokens = tokenizeProseAbbreviations(text, { firstOccurrenceOnly, seen })
  return (
    <>
      {tokens.map((token, idx) => {
        if (token.type === 'text') {
          return <span key={idx}>{token.value}</span>
        }
        const canonical = getAbbreviationExpansion(token.term) ?? token.expansion
        return (
          <span key={idx}>
            <Abbr title={canonical}>{token.term}</Abbr>
            {token.suffix}
          </span>
        )
      })}
    </>
  )
}
