export type MessageRole = 'user' | 'assistant'

export interface MessageDTO {
  id: string
  role: MessageRole
  content: string
  createdAt?: string
  model?: string
}

export interface ChatResponse {
  reply: string
  message?: MessageDTO
  history?: MessageDTO[]
  model?: string
  error?: string
}

export interface StatusDTO {
  online: boolean
  model: string
  baseUrl?: string
  gpuCount?: number
  error?: string
}

interface WailsAppBindings {
  SendMessage: (text: string) => Promise<unknown>
  GetHistory: () => Promise<unknown>
  ClearConversation: () => Promise<unknown>
  GetStatus: () => Promise<unknown>
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: WailsAppBindings
      }
    }
  }
}

function getWailsApp(): WailsAppBindings | null {
  if (typeof window !== 'undefined' && window.go?.main?.App) {
    return window.go.main.App
  }
  return null
}

function asRecord(val: unknown): Record<string, unknown> | null {
  if (val && typeof val === 'object' && !Array.isArray(val)) {
    return val as Record<string, unknown>
  }
  return null
}

export function normalizeRole(raw: unknown): MessageRole {
  const str = typeof raw === 'string' ? raw.trim().toLowerCase() : ''
  if (str === 'user' || str === 'human') {
    return 'user'
  }
  return 'assistant'
}

export function normalizeMessage(raw: unknown, fallbackIndex = 0): MessageDTO {
  const obj = asRecord(raw)
  if (!obj) {
    return {
      id: `msg-${fallbackIndex}`,
      role: 'assistant',
      content: typeof raw === 'string' ? raw : '',
    }
  }

  const idRaw = obj.id ?? obj.ID ?? `msg-${fallbackIndex}`
  const role = normalizeRole(obj.role ?? obj.Role ?? obj.type)
  const contentRaw = obj.content ?? obj.Content ?? obj.text ?? obj.Text ?? obj.reply ?? ''
  const createdAtRaw = obj.created_at ?? obj.createdAt ?? obj.CreatedAt ?? obj.timestamp
  const modelRaw = obj.model ?? obj.Model ?? obj.model_alias

  return {
    id: String(idRaw),
    role,
    content: typeof contentRaw === 'string' ? contentRaw : String(contentRaw ?? ''),
    createdAt: typeof createdAtRaw === 'string' ? createdAtRaw : undefined,
    model: typeof modelRaw === 'string' ? modelRaw : undefined,
  }
}

export function normalizeHistory(raw: unknown): MessageDTO[] {
  if (Array.isArray(raw)) {
    return raw.map((item, idx) => normalizeMessage(item, idx))
  }
  const obj = asRecord(raw)
  if (obj && Array.isArray(obj.messages)) {
    return obj.messages.map((item, idx) => normalizeMessage(item, idx))
  }
  if (obj && Array.isArray(obj.history)) {
    return obj.history.map((item, idx) => normalizeMessage(item, idx))
  }
  return []
}

export function normalizeChatResponse(raw: unknown): ChatResponse {
  if (typeof raw === 'string') {
    return { reply: raw }
  }
  const obj = asRecord(raw)
  if (!obj) {
    return { reply: '' }
  }

  if (typeof obj.error === 'string' && obj.error.trim().length > 0 && !obj.reply && !obj.content) {
    throw new Error(obj.error)
  }

  const nestedMsg = asRecord(obj.message)
  const replyRaw =
    obj.reply ??
    obj.Reply ??
    obj.content ??
    obj.Content ??
    obj.response ??
    nestedMsg?.content ??
    nestedMsg?.text ??
    ''
  const reply = typeof replyRaw === 'string' ? replyRaw : String(replyRaw ?? '')
  const modelRaw = obj.model ?? obj.Model ?? obj.model_alias ?? nestedMsg?.model
  const history =
    Array.isArray(obj.history) || Array.isArray(obj.messages)
      ? normalizeHistory(obj.history ?? obj.messages)
      : undefined

  const normalizedMsg = nestedMsg ? normalizeMessage(nestedMsg, Date.now()) : undefined
  if (normalizedMsg && !normalizedMsg.model && typeof modelRaw === 'string') {
    normalizedMsg.model = modelRaw
  }

  return {
    reply,
    message: normalizedMsg,
    history,
    model: typeof modelRaw === 'string' ? modelRaw : undefined,
  }
}

export function normalizeStatus(raw: unknown): StatusDTO {
  const obj = asRecord(raw)
  if (!obj) {
    return {
      online: false,
      model: 'gpu-assistant',
      error: 'Unable to parse proxy status',
    }
  }

  const onlineRaw =
    obj.online ??
    obj.Online ??
    obj.proxy_online ??
    obj.proxyOnline ??
    obj.litellm_online ??
    obj.reachable ??
    obj.healthy ??
    false
  const modelRaw =
    obj.model ?? obj.Model ?? obj.model_alias ?? obj.modelAlias ?? obj.litellm_model ?? 'gpu-assistant'
  const baseUrlRaw = obj.base_url ?? obj.baseUrl ?? obj.litellm_url ?? obj.proxy_url
  const gpuCountRaw = obj.gpu_count ?? obj.gpuCount ?? obj.gpus
  const errorRaw = obj.error ?? obj.Error ?? obj.message

  return {
    online: Boolean(onlineRaw),
    model: typeof modelRaw === 'string' && modelRaw.trim() ? modelRaw.trim() : 'gpu-assistant',
    baseUrl: typeof baseUrlRaw === 'string' ? baseUrlRaw : undefined,
    gpuCount: typeof gpuCountRaw === 'number' ? gpuCountRaw : undefined,
    error: typeof errorRaw === 'string' && errorRaw.trim() ? errorRaw.trim() : undefined,
  }
}

async function fetchJson(url: string, init?: RequestInit): Promise<unknown> {
  const response = await fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })

  const text = await response.text()
  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = text
    }
  }

  if (!response.ok) {
    const obj = asRecord(parsed)
    const errMsg =
      (typeof obj?.error === 'string' && obj.error) ||
      (typeof obj?.message === 'string' && obj.message) ||
      (typeof parsed === 'string' && parsed) ||
      `Request failed (${response.status})`
    throw new Error(errMsg)
  }

  return parsed
}

export async function sendMessage(text: string): Promise<ChatResponse> {
  const wails = getWailsApp()
  if (wails) {
    const raw = await wails.SendMessage(text)
    return normalizeChatResponse(raw)
  }
  const raw = await fetchJson('/api/chat', {
    method: 'POST',
    body: JSON.stringify({ text, message: text }),
  })
  return normalizeChatResponse(raw)
}

export async function getHistory(): Promise<MessageDTO[]> {
  const wails = getWailsApp()
  if (wails) {
    const raw = await wails.GetHistory()
    return normalizeHistory(raw)
  }
  const raw = await fetchJson('/api/history', { method: 'GET' })
  return normalizeHistory(raw)
}

export async function clearConversation(): Promise<void> {
  const wails = getWailsApp()
  if (wails) {
    await wails.ClearConversation()
    return
  }
  await fetchJson('/api/clear', { method: 'POST' })
}

export async function getStatus(): Promise<StatusDTO> {
  const wails = getWailsApp()
  if (wails) {
    const raw = await wails.GetStatus()
    return normalizeStatus(raw)
  }
  const raw = await fetchJson('/api/status', { method: 'GET' })
  return normalizeStatus(raw)
}
