import { useEffect, useRef, type ReactNode } from 'react'
import type { MessageDTO } from '../lib/wails.ts'
import { Abbreviated } from './Abbr.tsx'
import { STAR_SIGIL_PATH, type OxideSlot } from './StatusHeader.tsx'

export interface StarterPrompt {
  id: string
  tag: string
  prompt: string
}

export const STARTER_PROMPTS: readonly StarterPrompt[] = Object.freeze([
  {
    id: 'pt-list',
    tag: 'PT · 01',
    prompt: 'Quais GPUs existem na infraestrutura?',
  },
  {
    id: 'pt-a100',
    tag: 'PT · 02',
    prompt: 'Existem duas A100 disponíveis?',
  },
  {
    id: 'en-memory',
    tag: 'EN · 03',
    prompt: 'Which GPU has the most memory available?',
  },
  {
    id: 'en-b200',
    tag: 'EN · 04',
    prompt: 'Do you have a B200?',
  },
])

/**
 * Render inline markdown (`**bold**` and `` `code` ``).
 * When `allowAbbreviations` is true (pure body prose paragraphs), plain text segments
 * outside inline code are passed through `<Abbreviated>`.
 */
function renderInlineProse(
  text: string,
  allowAbbreviations: boolean,
  seenAbbreviations?: Set<string>,
): ReactNode[] {
  const nodes: ReactNode[] = []
  const inlinePattern = /(\*\*[^*]+\*\*|`[^`]+`)/g
  let lastIdx = 0
  let match: RegExpExecArray | null = inlinePattern.exec(text)
  let keyCounter = 0

  while (match !== null) {
    const token = match[0] ?? ''
    const idx = match.index
    if (idx > lastIdx) {
      const slice = text.slice(lastIdx, idx)
      nodes.push(
        allowAbbreviations ? (
          <Abbreviated key={`t-${keyCounter++}`} text={slice} seen={seenAbbreviations} />
        ) : (
          <span key={`t-${keyCounter++}`}>{slice}</span>
        ),
      )
    }

    if (token.startsWith('**') && token.endsWith('**')) {
      const inner = token.slice(2, -2)
      nodes.push(
        <strong key={`b-${keyCounter++}`}>
          {allowAbbreviations ? (
            <Abbreviated text={inner} seen={seenAbbreviations} />
          ) : (
            inner
          )}
        </strong>,
      )
    } else if (token.startsWith('`') && token.endsWith('`')) {
      const codeText = token.slice(1, -1)
      // Never wrap code identifiers in <abbr>
      nodes.push(
        <code key={`c-${keyCounter++}`} className="message-inline-code">
          {codeText}
        </code>,
      )
    }

    lastIdx = idx + token.length
    match = inlinePattern.exec(text)
  }

  if (lastIdx < text.length) {
    const tail = text.slice(lastIdx)
    nodes.push(
      allowAbbreviations ? (
        <Abbreviated key={`t-${keyCounter++}`} text={tail} seen={seenAbbreviations} />
      ) : (
        <span key={`t-${keyCounter++}`}>{tail}</span>
      ),
    )
  }

  return nodes
}

function parseTableCells(line: string): string[] {
  const trimmed = line.trim().replace(/^\|/, '').replace(/\|$/, '')
  return trimmed.split('|').map((cell) => cell.trim())
}

function isTableSeparatorLine(line: string): boolean {
  const trimmed = line.trim()
  if (!trimmed.includes('|') || !trimmed.includes('-')) return false
  const cells = parseTableCells(trimmed)
  return cells.length > 0 && cells.every((c) => /^:?-{2,}:?$/.test(c))
}

/**
 * Structured markdown renderer for assistant and user messages.
 * Supports fenced code blocks, GFM tables, bullet/numbered lists, bold text, and inline code.
 * Pure body prose paragraphs pass through `<Abbreviated>` while tables, code, and labels stay free of `<abbr>`.
 */
export function renderMarkdownBlocks(
  content: string,
  allowProseAbbreviations: boolean,
): ReactNode[] {
  const blocks: ReactNode[] = []
  const seenAbbreviations = new Set<string>()
  const segments = content.split(/(```[\s\S]*?```)/g)
  let blockKey = 0

  for (const segment of segments) {
    if (!segment) continue

    if (segment.startsWith('```') && segment.endsWith('```')) {
      const firstNewline = segment.indexOf('\n')
      const codeBody =
        firstNewline !== -1 ? segment.slice(firstNewline + 1, -3).replace(/\n$/, '') : segment.slice(3, -3)
      blocks.push(
        <pre key={`code-${blockKey++}`} className="message-code-block">
          <code>{codeBody}</code>
        </pre>,
      )
      continue
    }

    const lines = segment.split(/\r?\n/)
    let i = 0

    while (i < lines.length) {
      const line = lines[i] ?? ''
      const trimmed = line.trim()

      if (!trimmed) {
        i++
        continue
      }

      // Check for Markdown table (header row + separator row)
      const nextLine = i + 1 < lines.length ? (lines[i + 1] ?? '') : ''
      if (trimmed.includes('|') && isTableSeparatorLine(nextLine)) {
        const headers = parseTableCells(trimmed)
        i += 2
        const rows: string[][] = []
        while (i < lines.length) {
          const rowLine = (lines[i] ?? '').trim()
          if (!rowLine || !rowLine.includes('|')) break
          rows.push(parseTableCells(rowLine))
          i++
        }
        blocks.push(
          <div key={`tbl-${blockKey++}`} className="message-table-wrap">
            <table className="message-table">
              <thead>
                <tr>
                  {headers.map((header, hIdx) => (
                    <th key={hIdx}>{header}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row, rIdx) => (
                  <tr key={rIdx}>
                    {row.map((cell, cIdx) => (
                      <td key={cIdx}>{renderInlineProse(cell, false)}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>,
        )
        continue
      }

      // Check for unordered list (- or *)
      if (/^[-*]\s+/.test(trimmed)) {
        const items: string[] = []
        while (i < lines.length) {
          const itemLine = (lines[i] ?? '').trim()
          if (!/^[-*]\s+/.test(itemLine)) break
          items.push(itemLine.replace(/^[-*]\s+/, ''))
          i++
        }
        blocks.push(
          <ul key={`ul-${blockKey++}`}>
            {items.map((item, idx) => (
              <li key={idx}>
                {renderInlineProse(item, allowProseAbbreviations, seenAbbreviations)}
              </li>
            ))}
          </ul>,
        )
        continue
      }

      // Check for ordered list (1. 2. ...)
      if (/^\d+\.\s+/.test(trimmed)) {
        const items: string[] = []
        while (i < lines.length) {
          const itemLine = (lines[i] ?? '').trim()
          if (!/^\d+\.\s+/.test(itemLine)) break
          items.push(itemLine.replace(/^\d+\.\s+/, ''))
          i++
        }
        blocks.push(
          <ol key={`ol-${blockKey++}`}>
            {items.map((item, idx) => (
              <li key={idx}>
                {renderInlineProse(item, allowProseAbbreviations, seenAbbreviations)}
              </li>
            ))}
          </ol>,
        )
        continue
      }

      // Markdown heading lines (# ...) rendered as bold paragraph text so headings never contain <abbr>
      if (/^#{1,6}\s+/.test(trimmed)) {
        const headingText = trimmed.replace(/^#{1,6}\s+/, '')
        blocks.push(
          <p key={`hd-${blockKey++}`}>
            <strong>{headingText}</strong>
          </p>,
        )
        i++
        continue
      }

      // Standard body prose paragraph (collect consecutive non-empty prose lines)
      const paraLines: string[] = []
      while (i < lines.length) {
        const candidate = (lines[i] ?? '').trim()
        const candidateNext = i + 1 < lines.length ? (lines[i + 1] ?? '') : ''
        if (
          !candidate ||
          /^[-*]\s+/.test(candidate) ||
          /^\d+\.\s+/.test(candidate) ||
          /^#{1,6}\s+/.test(candidate) ||
          (candidate.includes('|') && isTableSeparatorLine(candidateNext))
        ) {
          break
        }
        paraLines.push(candidate)
        i++
      }

      if (paraLines.length > 0) {
        const joined = paraLines.join(' ')
        blocks.push(
          <p key={`p-${blockKey++}`}>
            {renderInlineProse(joined, allowProseAbbreviations, seenAbbreviations)}
          </p>,
        )
      }
    }
  }

  return blocks
}

export interface MessageListProps {
  messages: MessageDTO[]
  isLoading: boolean
  errorMessage: string | null
  canRetry: boolean
  lastCompletedMessageId: string | null
  activeOxideSlot: OxideSlot
  onSelectPrompt: (prompt: string) => void
  onRetry: () => void
  onDismissError: () => void
}

export function MessageList({
  messages,
  isLoading,
  errorMessage,
  canRetry,
  lastCompletedMessageId,
  activeOxideSlot,
  onSelectPrompt,
  onRetry,
  onDismissError,
}: MessageListProps) {
  const endRef = useRef<HTMLDivElement | null>(null)
  const useOxideForError = activeOxideSlot === 'error-banner'

  useEffect(() => {
    if (typeof endRef.current?.scrollIntoView === 'function') {
      endRef.current.scrollIntoView({ block: 'nearest' })
    }
  }, [messages.length, isLoading])

  const turnCountLabel = `${messages.length} ${messages.length === 1 ? 'TURN' : 'TURNS'}`

  return (
    <>
      <div className="chat-section-header">
        <h2 className="chat-section-title">Chat</h2>
        <span className="chat-counter">{turnCountLabel}</span>
      </div>

      {errorMessage ? (
        <div
          role="alert"
          className={`error-banner ${useOxideForError ? 'error-banner-oxide' : ''}`.trim()}
        >
          <div className="error-banner-content">
            <span
              className={`error-banner-label ${
                useOxideForError ? 'error-banner-label-oxide' : ''
              }`.trim()}
            >
              PROXY ALERT
            </span>
            <p className="error-banner-text">{errorMessage}</p>
          </div>
          <div className="error-banner-actions">
            {canRetry ? (
              <button
                type="button"
                className="btn btn-primary error-banner-btn"
                onClick={onRetry}
                disabled={isLoading}
              >
                Retry
              </button>
            ) : null}
            <button
              type="button"
              className="btn btn-ghost error-banner-btn"
              onClick={onDismissError}
            >
              Dismiss
            </button>
          </div>
        </div>
      ) : null}

      <div className="message-stream" role="log" aria-live="polite" aria-busy={isLoading}>
        {messages.length === 0 && !isLoading ? (
          <div className="empty-state">
            <div className="empty-state-intro">
              <p className="empty-state-heading">Ask about the lab nodes</p>
              <p className="empty-state-copy">
                <Abbreviated text="I query our local SQLite catalog of 5 NVIDIA GPU models to verify VRAM capacity, MIG partitioning, ECC support, and live node availability." />
              </p>
            </div>
            <div className="starter-grid">
              {STARTER_PROMPTS.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className="btn starter-chip"
                  onClick={() => onSelectPrompt(item.prompt)}
                >
                  <span className="starter-chip-tag">{item.tag}</span>
                  <span>{item.prompt}</span>
                </button>
              ))}
            </div>
          </div>
        ) : null}

        {messages.map((msg) => {
          const isUser = msg.role === 'user'
          const isNewlyCompleted = !isUser && msg.id === lastCompletedMessageId
          return (
            <article
              key={msg.id}
              className={`message-card ${
                isUser ? 'message-card-user' : 'message-card-assistant'
              }`}
            >
              <div className="message-meta">
                <span className={`message-role ${isUser ? 'message-role-user' : ''}`.trim()}>
                  {isUser ? 'USER' : 'ASSISTANT'}
                </span>
                <span className={isNewlyCompleted ? 'message-badge-pulse' : ''}>
                  {isUser ? 'LOCAL' : msg.model || 'gpu-assistant'}
                </span>
              </div>
              <div className="message-body">
                {renderMarkdownBlocks(msg.content, !isUser)}
              </div>
            </article>
          )
        })}

        {isLoading ? (
          <div className="message-card message-card-assistant" role="status">
            <div className="loader-row">
              <svg
                className="pixel-star-loader"
                viewBox="0 0 1024 1024"
                aria-hidden="true"
                focusable="false"
                data-star-mark="loader"
              >
                <path d={STAR_SIGIL_PATH} fill="currentColor" />
              </svg>
              <span className="loader-label">QUERYING INVENTORY</span>
            </div>
          </div>
        ) : null}

        <div ref={endRef} />
      </div>
    </>
  )
}
