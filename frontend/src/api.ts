import type { AppState, AuthUser, DatabaseDriver } from './types'

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? 'http://localhost:8080/api'
const TOKEN_KEY = 'waitwhat-auth-token'

function authHeaders() {
  const token = localStorage.getItem(TOKEN_KEY)
  return token ? ({ Authorization: `Bearer ${token}` } as Record<string, string>) : {}
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...authHeaders(),
    ...((init.headers as Record<string, string> | undefined) ?? {})
  }
  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers
  })

  if (!response.ok) {
    const data = await response.json().catch(() => ({ error: '请求失败' }))
    throw new Error(data.error ?? '请求失败')
  }

  return response.json()
}

export function saveToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

export function hasToken() {
  return Boolean(localStorage.getItem(TOKEN_KEY))
}

export interface InitDatabasePayload {
  driver: DatabaseDriver
  sqlitePath: string
  pgHost: string
  pgPort: number
  pgDatabase: string
  pgUser: string
  pgPassword: string
  pgSslMode: string
}

export interface AuthPayload {
  username: string
  password: string
  email?: string
  name?: string
}

export interface AuthResult {
  token: string
  user: AuthUser
}

export interface InitDatabasePayloadResponse {
  message: string
}

export interface CreateEventPayload {
  userId: number
  title: string
  content: string
  eventAt: string
  reminderEnabled: boolean
  recurrenceType: 'once' | 'daily' | 'workday' | 'cron'
  recurrenceExpr: string
  reminderPoints: Array<{ id: number; label: string; offsetMin: number }>
  boundChannelIds: number[]
  boundGroupIds: number[]
}

export interface SaveMailConfigPayload {
  enabled: boolean
  host: string
  port: number
  username: string
  password: string
  fromName: string
  fromAddress: string
  useTls: boolean
  useSsl: boolean
}

export interface SaveDingTalkConfigPayload {
  enabled: boolean
  webhook: string
  secret: string
  useSign: boolean
  keyword: string
}

export interface SaveNotifyGroupPayload {
  id?: number
  name: string
  enabled: boolean
  members: Array<{
    type: 'email' | 'dingtalk_webhook'
    label: string
    target: string
    secret?: string
    keyword?: string
    useSign: boolean
    enabled: boolean
  }>
}

export interface MailDiagnoseStep {
  port: number
  mode: string
  step: string
  ok: boolean
  latencyMs: number
  error?: string
}

export interface MailDiagnoseResult {
  host: string
  steps: MailDiagnoseStep[]
}

export async function fetchBootstrap(): Promise<AppState> {
  return request<AppState>('/bootstrap', { method: 'GET', headers: authHeaders() })
}

export async function initDatabase(payload: InitDatabasePayload) {
  return request('/database/init', { method: 'POST', body: JSON.stringify(payload) })
}

export async function resetDatabase() {
  return request('/database/reset', { method: 'POST' })
}

export async function setupAdmin(payload: AuthPayload): Promise<AuthResult> {
  return request<AuthResult>('/auth/setup-admin', { method: 'POST', body: JSON.stringify(payload) })
}

export async function login(payload: AuthPayload): Promise<AuthResult> {
  return request<AuthResult>('/auth/login', { method: 'POST', body: JSON.stringify(payload) })
}

export async function register(payload: AuthPayload): Promise<AuthResult> {
  return request<AuthResult>('/auth/register', { method: 'POST', body: JSON.stringify(payload) })
}

export async function fetchMe() {
  return request<{ user: AuthUser }>('/auth/me', { method: 'GET' })
}

export async function logout() {
  return request('/auth/logout', { method: 'POST' })
}

export async function createEvent(payload: CreateEventPayload) {
  return request('/events', { method: 'POST', body: JSON.stringify(payload) })
}

export async function updateEvent(eventId: number, payload: CreateEventPayload) {
  return request(`/events/${eventId}`, { method: 'PUT', body: JSON.stringify(payload) })
}

export async function deleteEvent(eventId: number) {
  return request(`/events/${eventId}`, { method: 'DELETE' })
}

export async function saveMailConfig(payload: SaveMailConfigPayload) {
  return request('/mail/config', { method: 'POST', body: JSON.stringify(payload) })
}

export async function sendTestMail(to: string) {
  return request('/mail/test', { method: 'POST', body: JSON.stringify({ to }) })
}

export async function diagnoseMail(host: string) {
  return request<{ message: string; result: MailDiagnoseResult }>('/mail/diagnose', {
    method: 'POST',
    body: JSON.stringify({ host })
  })
}

export async function dispatchReminders() {
  return request<{ message: string; result: { triggered: number; sent: number; failed: number; skipped: number; retried: number } }>(
    '/reminders/dispatch',
    { method: 'POST' }
  )
}

export async function saveDingTalkConfig(payload: SaveDingTalkConfigPayload) {
  return request('/dingtalk/config', { method: 'POST', body: JSON.stringify(payload) })
}

export async function sendTestDingTalk(payload: { webhook: string; secret: string; keyword?: string; useSign: boolean }) {
  return request('/dingtalk/test', { method: 'POST', body: JSON.stringify(payload) })
}

export async function saveNotifyGroup(payload: SaveNotifyGroupPayload) {
  return request('/notify-groups', { method: 'POST', body: JSON.stringify(payload) })
}

export async function deleteNotifyGroup(id: number) {
  return request(`/notify-groups/${id}`, { method: 'DELETE' })
}

export async function adminListUsers() {
  return request<{ users: AuthUser[] }>('/admin/users', { method: 'GET' })
}

export async function adminUpdateUserPassword(userId: number, password: string) {
  return request<{ message: string }>(`/admin/users/${userId}`, { method: 'PUT', body: JSON.stringify({ password }) })
}

export async function adminDeleteUser(userId: number) {
  return request<{ message: string }>(`/admin/users/${userId}`, { method: 'DELETE' })
}

export async function adminUpdateLoginPolicy(payload: { loginMaxFailed: number; loginWindowSec: number }) {
  return request<{ message: string; policy: { loginMaxFailed: number; loginWindowSec: number } }>('/admin/login-policy', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export async function adminListAuditLogs(limit: number, offset: number) {
  return request<{ items: Array<{
    id: number
    actorUserId: number
    actorUsername: string
    action: string
    targetUserId: number
    targetUsername: string
    detail: string
    createdAt: string
  }>; total: number }>(`/admin/audit-logs?limit=${limit}&offset=${offset}`, {
    method: 'GET'
  })
}
