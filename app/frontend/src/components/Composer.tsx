import { useRef, type FormEvent, type KeyboardEvent } from 'react'

export interface ComposerProps {
  value: string
  isLoading: boolean
  onChange: (next: string) => void
  onSubmit: () => void
}

export function Composer({ value, isLoading, onChange, onSubmit }: ComposerProps) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)

  const handleFormSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!value.trim() || isLoading) return
    onSubmit()
  }

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      if (!value.trim() || isLoading) return
      onSubmit()
    }
  }

  const canSend = value.trim().length > 0 && !isLoading

  return (
    <form className="composer-form" onSubmit={handleFormSubmit} aria-label="Send a message">
      <textarea
        ref={textareaRef}
        className="composer-input"
        rows={1}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="Ask in Portuguese or English about models, memory, or availability..."
        aria-label="Message input"
        disabled={isLoading}
      />
      <button
        type="submit"
        className="btn btn-primary composer-send-btn"
        disabled={!canSend}
        aria-label={isLoading ? 'Sending message' : 'Send message'}
      >
        {isLoading ? (
          <span className="three-cell-loader" aria-hidden="true">
            <span />
            <span />
            <span />
          </span>
        ) : null}
        <span>{isLoading ? 'Sending' : 'Send'}</span>
      </button>
    </form>
  )
}
