import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { describe, it } from 'node:test'
import { fileURLToPath } from 'node:url'

import {
  ABBREVIATION_GLOSSARY,
  getAbbreviationExpansion,
  tokenizeProseAbbreviations,
} from './abbreviations.ts'
import { IDENTITY_CONTACTS, buildRel, isIdentityUrl } from './links.ts'
import {
  normalizeChatResponse,
  normalizeHistory,
  normalizeMessage,
  normalizeRole,
  normalizeStatus,
} from './wails.ts'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const SRC_ROOT = path.resolve(__dirname, '..')

describe('Cobalt Lab v2 semantics, tokens, and Wails bridge', () => {
  describe('abbreviations.ts (pure body prose glossary)', () => {
    it('defines non-empty confirmed expansions for AI, GPU, VRAM, MIG, ECC, and LiS', () => {
      for (const term of ['AI', 'GPU', 'VRAM', 'MIG', 'ECC', 'LiS']) {
        const expansion = getAbbreviationExpansion(term)
        assert.ok(expansion && expansion.length > 3, `Missing expansion for ${term}`)
        assert.notEqual(expansion, term)
      }
      assert.equal(ABBREVIATION_GLOSSARY.NSESD, undefined)
      assert.equal(ABBREVIATION_GLOSSARY.HP, undefined)
      assert.equal(ABBREVIATION_GLOSSARY.DevOps, undefined)
    })

    it('tokenizes confirmed abbreviations and plurals in pure prose while preserving text', () => {
      const sentence =
        'Our NVIDIA GPUs provide 80 GB VRAM with MIG partitioning and ECC for AI workloads.'
      const tokens = tokenizeProseAbbreviations(sentence)
      const abbrTokens = tokens.filter((t) => t.type === 'abbr')

      assert.equal(abbrTokens.length, 5)
      assert.deepEqual(
        abbrTokens.map((t) => (t.type === 'abbr' ? `${t.term}${t.suffix}` : '')),
        ['GPUs', 'VRAM', 'MIG', 'ECC', 'AI'],
      )

      const reconstructed = tokens
        .map((t) => (t.type === 'text' ? t.value : `${t.term}${t.suffix}`))
        .join('')
      assert.equal(reconstructed, sentence)
    })

    it('supports firstOccurrenceOnly with a shared seen set and ignores hyphenated IDs or URLs', () => {
      const seen = new Set<string>()
      const first = tokenizeProseAbbreviations('One GPU and another GPU in gpu-assistant.', {
        firstOccurrenceOnly: true,
        seen,
      })
      const firstAbbrs = first.filter((t) => t.type === 'abbr')
      assert.equal(firstAbbrs.length, 1)

      const second = tokenizeProseAbbreviations('A third GPU at https://example.com/GPU.', {
        firstOccurrenceOnly: true,
        seen,
      })
      const secondAbbrs = second.filter((t) => t.type === 'abbr')
      assert.equal(secondAbbrs.length, 0)
    })
  })

  describe('links.ts (IndieWeb rel="me" identity links)', () => {
    it('marks GitHub and LinkedIn contacts with identity: true and builds rel="me noopener noreferrer"', () => {
      assert.ok(IDENTITY_CONTACTS.length >= 2)
      const github = IDENTITY_CONTACTS.find((c) => c.id === 'github')
      const linkedin = IDENTITY_CONTACTS.find((c) => c.id === 'linkedin')

      assert.ok(github, 'Expected GitHub contact entry')
      assert.ok(linkedin, 'Expected LinkedIn contact entry')
      assert.equal(github.identity, true)
      assert.equal(linkedin.identity, true)
      assert.equal(isIdentityUrl(github.href), true)
      assert.equal(isIdentityUrl(linkedin.href), true)

      assert.equal(
        buildRel({ href: github.href, target: '_blank', identity: github.identity }),
        'me noopener noreferrer',
      )
      assert.equal(
        buildRel({ href: linkedin.href, target: '_blank', identity: linkedin.identity }),
        'me noopener noreferrer',
      )
    })

    it('combines custom rel tokens without duplicating or adding me to non-identity links', () => {
      assert.equal(
        buildRel({
          href: 'https://github.com/thenickfer',
          target: '_blank',
          rel: 'external noopener',
        }),
        'me external noopener noreferrer',
      )
      assert.equal(
        buildRel({
          href: 'https://docs.nvidia.com/datacenter/',
          target: '_blank',
          identity: false,
        }),
        'noopener noreferrer',
      )
      assert.equal(buildRel({ href: 'mailto:nick@example.com' }), undefined)
      assert.equal(buildRel({ href: '/#/chat' }), undefined)
    })
  })

  describe('wails.ts (DTO normalization for Wails and HTTP fallback)', () => {
    it('normalizes roles, messages, history, chat responses, and proxy status', () => {
      assert.equal(normalizeRole('human'), 'user')
      assert.equal(normalizeRole('USER'), 'user')
      assert.equal(normalizeRole('ai'), 'assistant')
      assert.equal(normalizeRole('assistant'), 'assistant')

      const msg = normalizeMessage({ ID: 42, Role: 'human', Content: 'Do you have a B200?' })
      assert.equal(msg.id, '42')
      assert.equal(msg.role, 'user')
      assert.equal(msg.content, 'Do you have a B200?')

      const history = normalizeHistory({
        messages: [
          { id: '1', role: 'user', content: 'Existem duas A100 disponíveis?' },
          { id: '2', role: 'assistant', content: 'Sim, 2 de 4 disponíveis.', model: 'gpu-assistant' },
        ],
      })
      assert.equal(history.length, 2)
      assert.equal(history[1]?.model, 'gpu-assistant')

      const chat = normalizeChatResponse({
        reply: 'No, NVIDIA B200 is not in our infrastructure.',
        model_alias: 'gpu-assistant',
      })
      assert.equal(chat.reply, 'No, NVIDIA B200 is not in our infrastructure.')
      assert.equal(chat.model, 'gpu-assistant')

      const status = normalizeStatus({
        proxy_online: true,
        model_alias: 'gpu-assistant',
        gpu_count: 5,
      })
      assert.equal(status.online, true)
      assert.equal(status.model, 'gpu-assistant')
      assert.equal(status.gpuCount, 5)
    })
  })

  describe('Cobalt Lab v2 static design audit', () => {
    it('enforces tokens-only styling in app.css and custom components (no hardcoded hex or rgb)', () => {
      const filesToAudit = [
        path.join(SRC_ROOT, 'styles', 'app.css'),
        path.join(SRC_ROOT, 'App.tsx'),
        path.join(SRC_ROOT, 'components', 'Abbr.tsx'),
        path.join(SRC_ROOT, 'components', 'ChatPlate.tsx'),
        path.join(SRC_ROOT, 'components', 'StatusHeader.tsx'),
        path.join(SRC_ROOT, 'components', 'MessageList.tsx'),
        path.join(SRC_ROOT, 'components', 'Composer.tsx'),
      ]

      for (const filePath of filesToAudit) {
        const content = fs.readFileSync(filePath, 'utf8')
        const rel = path.relative(SRC_ROOT, filePath)
        assert.equal(
          /#[0-9a-fA-F]{3,8}\b/.test(content),
          false,
          `Found hardcoded hex color in ${rel}`,
        )
        assert.equal(
          /\brgba?\s*\(/.test(content),
          false,
          `Found hardcoded rgb()/rgba() color in ${rel}`,
        )
      }
    })

    it('keeps <Abbr> and <Abbreviated> out of headers, composer buttons, and headings', () => {
      const headerSource = fs.readFileSync(
        path.join(SRC_ROOT, 'components', 'StatusHeader.tsx'),
        'utf8',
      )
      const composerSource = fs.readFileSync(
        path.join(SRC_ROOT, 'components', 'Composer.tsx'),
        'utf8',
      )
      assert.equal(/<Abbr|<Abbreviated|<abbr/.test(headerSource), false)
      assert.equal(/<Abbr|<Abbreviated|<abbr/.test(composerSource), false)
    })

    it('keeps total star marks in the UI at most 2', () => {
      const componentFiles = [
        path.join(SRC_ROOT, 'App.tsx'),
        path.join(SRC_ROOT, 'components', 'StatusHeader.tsx'),
        path.join(SRC_ROOT, 'components', 'MessageList.tsx'),
        path.join(SRC_ROOT, 'components', 'Composer.tsx'),
      ]
      let starMarkCount = 0
      for (const filePath of componentFiles) {
        const content = fs.readFileSync(filePath, 'utf8')
        const matches = content.match(/data-star-mark=/g)
        if (matches) starMarkCount += matches.length
      }
      assert.ok(
        starMarkCount <= 2,
        `Expected at most 2 star marks across components, found ${starMarkCount}`,
      )
    })
  })
})
