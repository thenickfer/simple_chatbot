import { forwardRef } from 'react'
import type { StatusDTO } from '../lib/wails.ts'
import { ChatPlate } from './ChatPlate.tsx'

export type OxideSlot = 'confirm-clear' | 'error-banner' | 'status-offline' | 'none'

export interface StatusHeaderProps {
  status: StatusDTO
  statusLoading: boolean
  hasMessages: boolean
  isLoading: boolean
  isConfirmingClear: boolean
  activeOxideSlot: OxideSlot
  onRequestClear: () => void
  onConfirmClear: () => void
  onCancelClear: () => void
  onRefreshStatus: () => void
}

/**
 * Four-pointed needle-tip concave cubic star path from public/sigils/liquid-sigil-mask.svg
 * (d = 0.60, e = 0.015). Used as the header signature mark (Star #1 of <= 2 star budget).
 */
export const STAR_SIGIL_PATH =
  'M512.00,153.60 C517.38,368.64 655.36,506.62 870.40,512.00 C655.36,517.38 517.38,655.36 512.00,870.40 C506.62,655.36 368.64,517.38 153.60,512.00 C368.64,506.62 506.62,368.64 512.00,153.60Z'

export const StatusHeader = forwardRef<HTMLElement, StatusHeaderProps>(function StatusHeader(
  {
    status,
    statusLoading,
    hasMessages,
    isLoading,
    isConfirmingClear,
    activeOxideSlot,
    onRequestClear,
    onConfirmClear,
    onCancelClear,
    onRefreshStatus,
  },
  ref,
) {
  const useOxideForStatus = activeOxideSlot === 'status-offline'
  const useOxideForConfirm = activeOxideSlot === 'confirm-clear'

  const statusLabel = statusLoading ? 'CHECKING' : status.online ? 'ONLINE' : 'OFFLINE'
  const dotClass = statusLoading
    ? 'status-dot status-dot-muted'
    : status.online
      ? 'status-dot'
      : useOxideForStatus
        ? 'status-dot status-dot-oxide'
        : 'status-dot status-dot-muted'

  return (
    <ChatPlate ref={ref} as="header" className="header-plate" aria-label="Application header">
      <div className="header-brand">
        <svg
          className="header-star-mark"
          viewBox="0 0 1024 1024"
          aria-hidden="true"
          focusable="false"
          data-star-mark="header-signature"
        >
          <path d={STAR_SIGIL_PATH} fill="currentColor" />
        </svg>
        <div className="header-titles">
          <div className="header-title-row">
            <h1 className="header-title">GPU Infrastructure</h1>
            <span className="pixel-stamp">COBALT LAB</span>
          </div>
          <p className="header-subtitle">
            I check live SQLite inventory and node availability through LiteLLM.
          </p>
        </div>
      </div>

      <div className="header-controls">
        <button
          type="button"
          onClick={onRefreshStatus}
          className={`btn btn-ghost plate-inner status-pill ${
            useOxideForStatus ? 'status-pill-oxide' : ''
          }`.trim()}
          title={
            status.online
              ? `LiteLLM proxy reachable (${status.model})`
              : status.error || 'LiteLLM proxy unreachable — click to recheck'
          }
          aria-label={`LiteLLM proxy status: ${statusLabel}, model ${status.model}. Activate to refresh status.`}
        >
          <span className={dotClass} aria-hidden="true" />
          <span
            className={`status-state ${useOxideForStatus ? 'status-state-oxide' : ''}`.trim()}
          >
            {statusLabel}
          </span>
          <span className="status-model">· {status.model}</span>
        </button>

        {!isConfirmingClear ? (
          <button
            type="button"
            className="btn btn-ghost plate-inner"
            onClick={onRequestClear}
            disabled={!hasMessages || isLoading}
          >
            Clear
          </button>
        ) : (
          <>
            <button
              type="button"
              className={`btn plate-inner ${useOxideForConfirm ? 'btn-oxide' : 'btn-primary'}`}
              onClick={onConfirmClear}
              disabled={isLoading}
            >
              Confirm clear
            </button>
            <button
              type="button"
              className="btn btn-ghost plate-inner"
              onClick={onCancelClear}
              disabled={isLoading}
            >
              Cancel
            </button>
          </>
        )}
      </div>
    </ChatPlate>
  )
})
