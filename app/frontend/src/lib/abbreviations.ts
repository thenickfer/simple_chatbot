/**
 * Confirmed abbreviation glossary per docs/design/COBALT_LAB.md ("Semantics -> Abbreviations").
 * Only confirmed expansions are included. Ambiguous or unconfirmed terms (e.g. NSESD, HP)
 * and non-abbreviation product names (e.g. DevOps) are intentionally excluded.
 */
export const ABBREVIATION_GLOSSARY: Readonly<Record<string, string>> = Object.freeze({
  AI: 'Artificial Intelligence',
  GPU: 'Graphics Processing Unit',
  VRAM: 'Video Random-Access Memory',
  MIG: 'Multi-Instance GPU',
  ECC: 'Error Correction Codes',
  LiS: 'Laboratório de Inovação em Software',
})

export type AbbreviationToken =
  | { type: 'text'; value: string }
  | { type: 'abbr'; term: string; expansion: string; suffix: string }

const GLOSSARY_TERMS = Object.keys(ABBREVIATION_GLOSSARY).sort((a, b) => b.length - a.length)

// Match whole-word glossary terms (optionally followed by plural 's') outside URLs/identifiers
const TERM_PATTERN = new RegExp(`\\b(${GLOSSARY_TERMS.join('|')})(s?)\\b`, 'g')
const URL_LIKE_PREFIX = /(?:https?:\/\/|www\.|[A-Za-z0-9_/-]+\.[A-Za-z0-9_/-]*)$/

export interface TokenizeAbbreviationsOptions {
  /**
   * When true, only the first occurrence of each term in the provided `seen` set
   * (or within this string) is wrapped in `<abbr>`. Defaults to false so prose paragraphs
   * can wrap each natural occurrence or share a page-level `seen` set.
   */
  firstOccurrenceOnly?: boolean
  seen?: Set<string>
}

export function getAbbreviationExpansion(term: string): string | undefined {
  return ABBREVIATION_GLOSSARY[term]
}

export function tokenizeProseAbbreviations(
  text: string,
  options: TokenizeAbbreviationsOptions = {},
): AbbreviationToken[] {
  if (!text) return []

  const firstOnly = options.firstOccurrenceOnly ?? false
  const seen = options.seen ?? new Set<string>()
  const tokens: AbbreviationToken[] = []
  let lastIndex = 0

  TERM_PATTERN.lastIndex = 0
  let match: RegExpExecArray | null = TERM_PATTERN.exec(text)

  while (match !== null) {
    const fullMatch = match[0] ?? ''
    const term = match[1] ?? ''
    const suffix = match[2] ?? ''
    const matchIndex = match.index
    const expansion = ABBREVIATION_GLOSSARY[term]

    const prefixSlice = text.slice(Math.max(0, matchIndex - 32), matchIndex)
    const prevChar = matchIndex > 0 ? text[matchIndex - 1] : ''
    const nextChar =
      matchIndex + fullMatch.length < text.length ? text[matchIndex + fullMatch.length] : ''

    // Do not wrap terms embedded in URLs, hyphenated model IDs (e.g. gpu-assistant), or code identifiers
    const insideIdentifier =
      prevChar === '-' ||
      prevChar === '_' ||
      prevChar === '/' ||
      nextChar === '-' ||
      nextChar === '_' ||
      URL_LIKE_PREFIX.test(prefixSlice)

    const shouldWrap = Boolean(expansion) && !insideIdentifier && (!firstOnly || !seen.has(term))

    if (shouldWrap && expansion) {
      if (matchIndex > lastIndex) {
        tokens.push({
          type: 'text',
          value: text.slice(lastIndex, matchIndex),
        })
      }
      seen.add(term)
      tokens.push({
        type: 'abbr',
        term,
        expansion,
        suffix,
      })
      lastIndex = matchIndex + fullMatch.length
    }

    match = TERM_PATTERN.exec(text)
  }

  if (lastIndex < text.length) {
    tokens.push({
      type: 'text',
      value: text.slice(lastIndex),
    })
  }

  return tokens
}
