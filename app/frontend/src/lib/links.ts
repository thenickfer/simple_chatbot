/**
 * Identity link helpers per docs/design/COBALT_LAB.md ("Semantics -> Identity links").
 * Links to Nícolas's personal/professional profiles carry `rel="me"`, combined cleanly
 * with `noopener noreferrer` when opening in a new tab (`target="_blank"`).
 */

export interface ContactLinkItem {
  id: string
  label: string
  href: string
  identity: boolean
}

export const IDENTITY_CONTACTS: readonly ContactLinkItem[] = Object.freeze([
  {
    id: 'github',
    label: 'GitHub',
    href: 'https://github.com/thenickfer',
    identity: true,
  },
  {
    id: 'linkedin',
    label: 'LinkedIn',
    href: 'https://www.linkedin.com/in/thenickfer',
    identity: true,
  },
])

const IDENTITY_HOST_PATTERNS: readonly RegExp[] = [
  /^https?:\/\/(?:www\.)?github\.com\/thenickfer(?:\/.*)?$/i,
  /^https?:\/\/(?:[a-z]{2,3}\.)?linkedin\.com\/in\/thenickfer(?:\/.*)?$/i,
]

export function isIdentityUrl(href: string): boolean {
  const trimmed = href.trim()
  if (!trimmed || trimmed.startsWith('mailto:') || trimmed.startsWith('#') || trimmed.startsWith('/')) {
    return false
  }
  return IDENTITY_HOST_PATTERNS.some((pattern) => pattern.test(trimmed))
}

export interface BuildRelOptions {
  href?: string
  target?: string
  identity?: boolean
  rel?: string
}

export function buildRel(options: BuildRelOptions = {}): string | undefined {
  const tokens: string[] = []
  const addToken = (token: string) => {
    const clean = token.trim().toLowerCase()
    if (clean && !tokens.includes(clean)) {
      tokens.push(clean)
    }
  }

  const isMe = Boolean(options.identity) || (options.href ? isIdentityUrl(options.href) : false)
  if (isMe) {
    addToken('me')
  }

  if (options.rel) {
    for (const part of options.rel.split(/\s+/)) {
      addToken(part)
    }
  }

  if (options.target === '_blank') {
    addToken('noopener')
    addToken('noreferrer')
  }

  return tokens.length > 0 ? tokens.join(' ') : undefined
}
