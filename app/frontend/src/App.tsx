import { useCallback, useEffect, useRef, useState } from 'react'
import { CreatureField } from './components/creature-field/index.ts'
import { ChatPlate } from './components/ChatPlate.tsx'
import { Composer } from './components/Composer.tsx'
import { MessageList } from './components/MessageList.tsx'
import { StatusHeader, type OxideSlot } from './components/StatusHeader.tsx'
import { IDENTITY_CONTACTS, buildRel } from './lib/links.ts'
import {
  clearConversation,
  getHistory,
  getStatus,
  sendMessage,
  type MessageDTO,
  type StatusDTO,
} from './lib/wails.ts'

const ONLINE_POLL_MS = 10_000
const OFFLINE_INITIAL_BACKOFF_MS = 12_000
const OFFLINE_MAX_BACKOFF_MS = 30_000

export function App() {
  const headerPlateRef = useRef<HTMLElement | null>(null)
  const chatPlateRef = useRef<HTMLElement | null>(null)

  const [messages, setMessages] = useState<MessageDTO[]>([])
  const [inputValue, setInputValue] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [lastFailedPrompt, setLastFailedPrompt] = useState<string | null>(null)
  const [lastCompletedMessageId, setLastCompletedMessageId] = useState<string | null>(null)
  const [isConfirmingClear, setIsConfirmingClear] = useState(false)

  const [statusLoading, setStatusLoading] = useState(true)
  const [status, setStatus] = useState<StatusDTO>({
    online: false,
    model: 'gpu-assistant',
  })

  const refreshStatus = useCallback(async (): Promise<boolean> => {
    try {
      const next = await getStatus()
      setStatus(next)
      setStatusLoading(false)
      return next.online
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'LiteLLM proxy unreachable'
      setStatus((prev) => ({
        ...prev,
        online: false,
        error: msg,
      }))
      setStatusLoading(false)
      return false
    }
  }, [])

  // Restore persisted SQLite conversation history on startup
  useEffect(() => {
    let cancelled = false
    const loadHistory = async () => {
      try {
        const history = await getHistory()
        if (!cancelled) {
          setMessages(history)
        }
      } catch {
        // Initial history fetch may fail if dev backend is still starting
      }
    }
    void loadHistory()
    return () => {
      cancelled = true
    }
  }, [])

  // Poll LiteLLM proxy status on startup and periodically with backoff when offline
  useEffect(() => {
    let cancelled = false
    let timerId: ReturnType<typeof setTimeout> | null = null
    let offlineBackoff = OFFLINE_INITIAL_BACKOFF_MS

    const scheduleNext = (delayMs: number) => {
      if (cancelled) return
      timerId = setTimeout(async () => {
        if (cancelled) return
        const isOnline = await refreshStatus()
        if (isOnline) {
          offlineBackoff = OFFLINE_INITIAL_BACKOFF_MS
          scheduleNext(ONLINE_POLL_MS)
        } else {
          const nextDelay = offlineBackoff
          offlineBackoff = Math.min(OFFLINE_MAX_BACKOFF_MS, Math.round(offlineBackoff * 1.5))
          scheduleNext(nextDelay)
        }
      }, delayMs)
    }

    void refreshStatus().then((isOnline) => {
      scheduleNext(isOnline ? ONLINE_POLL_MS : offlineBackoff)
    })

    return () => {
      cancelled = true
      if (timerId !== null) {
        clearTimeout(timerId)
      }
    }
  }, [refreshStatus])

  const submitPrompt = useCallback(
    async (rawPrompt: string, appendUserTurn = true) => {
      const prompt = rawPrompt.trim()
      if (!prompt || isLoading) return

      setErrorMessage(null)
      setLastFailedPrompt(null)
      setIsConfirmingClear(false)
      setIsLoading(true)

      if (appendUserTurn) {
        const optimisticUserMessage: MessageDTO = {
          id: `user-${Date.now()}`,
          role: 'user',
          content: prompt,
          createdAt: new Date().toISOString(),
        }
        setMessages((prev) => [...prev, optimisticUserMessage])
      }

      try {
        const res = await sendMessage(prompt)
        const replyId = res.message?.id ?? `assistant-${Date.now()}`
        if (res.history && res.history.length > 0) {
          setMessages(res.history)
        } else {
          const assistantMessage: MessageDTO = res.message ?? {
            id: replyId,
            role: 'assistant',
            content: res.reply,
            model: res.model ?? status.model,
            createdAt: new Date().toISOString(),
          }
          setMessages((prev) => [...prev, assistantMessage])
        }
        setLastCompletedMessageId(replyId)
        setStatus((prev) => ({ ...prev, online: true, error: undefined }))
      } catch (err) {
        const message =
          err instanceof Error
            ? err.message
            : 'LiteLLM proxy request failed. Check that the proxy and model container are running.'
        setErrorMessage(message)
        setLastFailedPrompt(prompt)
        void refreshStatus()
      } finally {
        setIsLoading(false)
      }
    },
    [isLoading, refreshStatus, status.model],
  )

  const handleComposerSubmit = () => {
    const prompt = inputValue.trim()
    if (!prompt || isLoading) return
    setInputValue('')
    void submitPrompt(prompt, true)
  }

  const handleSelectStarterPrompt = (prompt: string) => {
    if (isLoading) return
    setInputValue('')
    void submitPrompt(prompt, true)
  }

  const handleRetry = () => {
    if (!lastFailedPrompt || isLoading) return
    void submitPrompt(lastFailedPrompt, false)
  }

  const handleConfirmClear = async () => {
    setIsConfirmingClear(false)
    setErrorMessage(null)
    setLastFailedPrompt(null)
    try {
      await clearConversation()
      setMessages([])
      setLastCompletedMessageId(null)
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Unable to clear conversation history in SQLite.'
      setErrorMessage(message)
    }
  }

  // Strictly enforce at most ONE oxide (--ds-color-oxide) highlight across the entire view
  const activeOxideSlot: OxideSlot = isConfirmingClear
    ? 'confirm-clear'
    : errorMessage !== null
      ? 'error-banner'
      : !statusLoading && !status.online
        ? 'status-offline'
        : 'none'

  return (
    <div className="app-shell">
      <CreatureField avoid={[headerPlateRef, chatPlateRef]} fixed />

      <StatusHeader
        ref={headerPlateRef}
        status={status}
        statusLoading={statusLoading}
        hasMessages={messages.length > 0}
        isLoading={isLoading}
        isConfirmingClear={isConfirmingClear}
        activeOxideSlot={activeOxideSlot}
        onRequestClear={() => setIsConfirmingClear(true)}
        onConfirmClear={() => void handleConfirmClear()}
        onCancelClear={() => setIsConfirmingClear(false)}
        onRefreshStatus={() => void refreshStatus()}
      />

      <ChatPlate ref={chatPlateRef} as="main" className="chat-plate" aria-label="Chat workspace">
        <MessageList
          messages={messages}
          isLoading={isLoading}
          errorMessage={errorMessage}
          canRetry={Boolean(lastFailedPrompt)}
          lastCompletedMessageId={lastCompletedMessageId}
          activeOxideSlot={activeOxideSlot}
          onSelectPrompt={handleSelectStarterPrompt}
          onRetry={handleRetry}
          onDismissError={() => {
            setErrorMessage(null)
            setLastFailedPrompt(null)
          }}
        />

        <Composer
          value={inputValue}
          isLoading={isLoading}
          onChange={setInputValue}
          onSubmit={handleComposerSubmit}
        />

        <footer className="footer-bar" aria-label="Lab author and profile links">
          <p className="footer-note">Nícolas da Costa Ferreira · SQLite + LiteLLM</p>
          <div className="footer-links">
            {IDENTITY_CONTACTS.map((contact) => (
              <a
                key={contact.id}
                href={contact.href}
                target="_blank"
                rel={buildRel({
                  href: contact.href,
                  target: '_blank',
                  identity: contact.identity,
                })}
                className="identity-link"
              >
                {contact.label}
              </a>
            ))}
          </div>
        </footer>
      </ChatPlate>
    </div>
  )
}
export default App
