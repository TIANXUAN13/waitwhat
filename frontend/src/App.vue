<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  adminDeleteUser,
  adminListAuditLogs,
  adminListUsers,
  adminUpdateLoginPolicy,
  adminUpdateUserPassword,
  clearToken,
  createEvent,
  deleteNotifyGroup,
  deleteEvent,
  diagnoseMail,
  dispatchReminders,
  fetchBootstrap,
  fetchMe,
  hasToken,
  initDatabase,
  login,
  logout as apiLogout,
  register,
  resetDatabase,
  saveMailConfig,
  saveNotifyGroup,
  saveToken,
  sendTestDingTalk,
  sendTestMail,
  setupAdmin,
  updateEvent
} from './api'
import type { AdminAuditLog, AppState, AuthUser, DatabaseDriver, NotificationGroup } from './types'

interface ReminderOption {
  key: string
  label: string
  offsetMin: number
  selected: boolean
}

type ViewMode = 'composer' | 'list' | 'notify' | 'users' | 'settings'
type AuthMode = 'login' | 'register'

const SETTINGS_KEY = 'waitwhat-ui-settings'
const LOGIN_READY_HINT_SEEN_KEY = 'waitwhat-login-ready-hint-seen'
const COMPOSER_DRAFT_KEY = 'waitwhat-composer-draft'

const loading = ref(true)
const submitting = ref(false)
const creating = ref(false)
const savingMail = ref(false)
const testingMail = ref(false)
const diagnosingMail = ref(false)
const dispatching = ref(false)
const savingUi = ref(false)
const authSubmitting = ref(false)
const savingLoginPolicy = ref(false)
const resettingUserPasswordId = ref(0)
const deletingUser = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const appState = ref<AppState | null>(null)
const currentView = ref<ViewMode>('composer')
const listFocus = ref<'pending' | 'expired'>('pending')
const listQuery = ref('')
const listSort = ref<'time_asc' | 'time_desc' | 'title_asc'>('time_asc')
const selectedEventIds = ref<number[]>([])
const pendingPage = ref(1)
const expiredPage = ref(1)
const listPageSize = ref(12)
const userPage = ref(1)
const userPageSize = ref(12)
const auditPage = ref(1)
const auditPageSize = ref(12)
const auditTotal = ref(0)
const loadingAudit = ref(false)
const editReturnContext = ref<{ tab: 'pending' | 'expired'; page: number } | null>(null)
const authMode = ref<AuthMode>('login')
const editingEventId = ref<number | null>(null)
const showLoginReadyHint = ref(false)
const showMailConfig = ref(true)
const showResetInit = computed(() => /no such column|SQL logic error|database/i.test(errorMessage.value))
const bootstrapError = ref('')
const backendUnavailable = computed(() => !loading.value && !appState.value && Boolean(bootstrapError.value))
const managedUsers = ref<AuthUser[]>([])
const adminAuditLogs = ref<AdminAuditLog[]>([])
const userEditorVisible = ref(false)
const editingManagedUserId = ref(0)

const dbForm = reactive({
  driver: '' as DatabaseDriver | '',
  sqlitePath: './data/waitwhat.sqlite',
  pgHost: '127.0.0.1',
  pgPort: 5432,
  pgDatabase: 'waitwhat',
  pgUser: 'postgres',
  pgPassword: '',
  pgSslMode: 'disable'
})

const adminForm = reactive({
  username: '',
  password: '',
  email: '',
  name: ''
})

const authForm = reactive({
  username: '',
  password: '',
  email: '',
  name: ''
})

const loginPolicyForm = reactive({
  loginMaxFailed: 5,
  loginWindowSec: 600
})

const userPasswordDraft = reactive<Record<number, string>>({})

const uiSettings = reactive({
  projectName: 'WaitWhat',
  slogan: '清新的提醒工作台',
  displayName: 'Chen',
  displaySubTitle: '专注你的每个关键节点'
})

function nowDateTimeLocal() {
  const now = new Date()
  const offsetMs = now.getTimezoneOffset() * 60 * 1000
  return new Date(now.getTime() - offsetMs).toISOString().slice(0, 16)
}

const reminderDraft = reactive({
  title: '新的事项提醒',
  content: '这里可以写会议准备、账单提醒、待办安排等。',
  eventAt: nowDateTimeLocal(),
  recurrenceType: 'once' as 'once' | 'daily' | 'workday' | 'cron',
  recurrenceExpr: '',
  testMailTo: ''
})

const cronDraft = reactive({
  minute: '0',
  hour: '9',
  day: '*',
  month: '*',
  weekday: '1-5'
})

const reminderOptions = reactive<ReminderOption[]>([
  { key: 'day-1', label: '提前 1 天', offsetMin: 1440, selected: false },
  { key: 'hour-2', label: '提前 2 小时', offsetMin: 120, selected: false },
  { key: 'min-10', label: '提前 10 分钟', offsetMin: 10, selected: false }
])
const customReminderDraft = reactive({
  value: 0,
  unit: 'minute' as 'minute' | 'hour' | 'day'
})

const selectedGroupIds = ref<number[]>([])

const mailForm = reactive({
  enabled: false,
  host: 'smtp.qq.com',
  port: 587,
  username: '',
  password: '',
  fromName: 'WaitWhat',
  fromAddress: '',
  useTls: true,
  useSsl: false
})

const mailPasswordSaved = ref(false)
const mailDiagnosis = ref<{ host: string; steps: Array<{ port: number; mode: string; step: string; ok: boolean; latencyMs: number; error?: string }> } | null>(null)
const savingGroup = ref(false)
const groupQuery = ref('')
const notifyEditorVisible = ref(false)
const applyingUrlState = ref(false)
const testingMemberKey = ref('')
const memberTestStatus = reactive<Record<string, { state: 'idle' | 'sending' | 'success' | 'error'; message: string }>>({})
const confirmState = reactive({
  visible: false,
  title: '',
  message: '',
  confirmText: '确定',
  cancelText: '取消',
  resolve: null as null | ((confirmed: boolean) => void)
})
const groupForm = reactive({
  id: 0,
  name: '',
  enabled: true,
  members: [{ type: 'email' as 'email' | 'dingtalk_webhook', label: '默认邮箱', target: '', secret: '', keyword: '提醒', useSign: false, enabled: true }]
})

function isRealInitializedAt(value?: string) {
  return Boolean(value && value !== '0001-01-01T00:00:00Z')
}

const databaseConfigured = computed(() => isRealInitializedAt(appState.value?.database.initializedAt))
const adminConfigured = computed(() => Boolean(appState.value?.auth.adminExists))
const currentUser = computed<AuthUser | null>(() => appState.value?.auth.currentUser ?? null)
const loggedIn = computed(() => Boolean(currentUser.value))
const isAdmin = computed(() => currentUser.value?.role === 'admin')
const notifyGroups = computed<NotificationGroup[]>(() => appState.value?.notifyGroups ?? [])
const events = computed(() => appState.value?.events ?? [])
const tasks = computed(() => appState.value?.tasks ?? [])
const logs = computed(() => appState.value?.logs ?? [])
function normalizedRecurrence(type?: string) {
  return (type || 'once').trim().toLowerCase()
}

const pendingEvents = computed(() =>
  events.value.filter((event) => normalizedRecurrence(event.recurrenceType) !== 'once' || new Date(event.eventAt).getTime() >= Date.now())
)
const expiredEvents = computed(() =>
  events.value.filter((event) => normalizedRecurrence(event.recurrenceType) === 'once' && new Date(event.eventAt).getTime() < Date.now())
)
const filteredPendingEvents = computed(() => {
  const keyword = listQuery.value.trim().toLowerCase()
  const base = keyword
    ? pendingEvents.value.filter((event) => event.title.toLowerCase().includes(keyword) || event.content.toLowerCase().includes(keyword))
    : pendingEvents.value
  return [...base].sort((a, b) => {
    if (listSort.value === 'time_desc') return new Date(b.eventAt).getTime() - new Date(a.eventAt).getTime()
    if (listSort.value === 'title_asc') return a.title.localeCompare(b.title, 'zh-CN')
    return new Date(a.eventAt).getTime() - new Date(b.eventAt).getTime()
  })
})
const filteredExpiredEvents = computed(() => {
  const keyword = listQuery.value.trim().toLowerCase()
  const base = keyword
    ? expiredEvents.value.filter((event) => event.title.toLowerCase().includes(keyword) || event.content.toLowerCase().includes(keyword))
    : expiredEvents.value
  return [...base].sort((a, b) => {
    if (listSort.value === 'time_asc') return new Date(a.eventAt).getTime() - new Date(b.eventAt).getTime()
    if (listSort.value === 'title_asc') return a.title.localeCompare(b.title, 'zh-CN')
    return new Date(b.eventAt).getTime() - new Date(a.eventAt).getTime()
  })
})
const filteredNotifyGroups = computed(() => {
  const keyword = groupQuery.value.trim().toLowerCase()
  if (!keyword) return notifyGroups.value
  return notifyGroups.value.filter((group) => {
    if (group.name.toLowerCase().includes(keyword)) return true
    return group.members.some((member) => member.label.toLowerCase().includes(keyword) || member.target.toLowerCase().includes(keyword))
  })
})

const pendingTotalPages = computed(() => Math.max(1, Math.ceil(filteredPendingEvents.value.length / listPageSize.value)))
const expiredTotalPages = computed(() => Math.max(1, Math.ceil(filteredExpiredEvents.value.length / listPageSize.value)))
const userTotalPages = computed(() => Math.max(1, Math.ceil(managedUsers.value.length / userPageSize.value)))
const auditTotalPages = computed(() => Math.max(1, Math.ceil(auditTotal.value / auditPageSize.value)))
const pagedPendingEvents = computed(() => {
  const start = (pendingPage.value - 1) * listPageSize.value
  return filteredPendingEvents.value.slice(start, start + listPageSize.value)
})
const pagedExpiredEvents = computed(() => {
  const start = (expiredPage.value - 1) * listPageSize.value
  return filteredExpiredEvents.value.slice(start, start + listPageSize.value)
})
const pagedManagedUsers = computed(() => {
  const start = (userPage.value - 1) * userPageSize.value
  return managedUsers.value.slice(start, start + userPageSize.value)
})
const currentTabEvents = computed(() => (listFocus.value === 'pending' ? filteredPendingEvents.value : filteredExpiredEvents.value))
const currentPageEvents = computed(() => (listFocus.value === 'pending' ? pagedPendingEvents.value : pagedExpiredEvents.value))
const allCurrentPageSelected = computed(() => {
  const ids = currentPageEvents.value.map((item) => item.id)
  return ids.length > 0 && ids.every((id) => selectedEventIds.value.includes(id))
})
const editingManagedUser = computed(() => managedUsers.value.find((item) => item.id === editingManagedUserId.value) || null)
const groupDraftSnapshot = ref('')
const isGroupDirty = computed(() => groupDraftSnapshot.value !== serializeGroupDraft())

const selectedReminderSummary = computed(() => {
  const items = reminderOptions.filter((item) => item.selected).map((item) => item.label)
  items.push('到点提醒')
  return items
})

const selectedGroups = computed(() => notifyGroups.value.filter((group) => selectedGroupIds.value.includes(group.id)))

const summaryStats = computed(() => ({
  total: events.value.length,
  pending: tasks.value.filter((task) => task.status === 'pending').length,
  success: logs.value.filter((log) => log.status === 'success').length
}))

const recurrenceSummary = computed(() => {
  if (reminderDraft.recurrenceType === 'daily') return '每天'
  if (reminderDraft.recurrenceType === 'workday') return '工作日（周一到周五）'
  if (reminderDraft.recurrenceType === 'cron') {
    return reminderDraft.recurrenceExpr.trim() ? `Cron: ${reminderDraft.recurrenceExpr.trim()}` : 'Cron（未填写表达式）'
  }
  return '一次性'
})

const emailReady = computed(() => Boolean(mailForm.enabled && mailForm.host && mailForm.fromAddress))

function renderRecurrence(type?: string, expr?: string) {
  const value = normalizedRecurrence(type)
  if (value === 'daily') return '每天'
  if (value === 'workday') return '工作日'
  if (value === 'cron') return expr ? `Cron: ${expr}` : 'Cron'
  return '一次性'
}

const composerEventAtText = computed(() => {
  if (reminderDraft.recurrenceType === 'cron') {
    return '由 Cron 表达式决定触发时间'
  }
  return reminderDraft.eventAt || '请选择时间'
})

const cronPreview = computed(() => `${cronDraft.minute} ${cronDraft.hour} ${cronDraft.day} ${cronDraft.month} ${cronDraft.weekday}`)

function resetMessages() {
  errorMessage.value = ''
  successMessage.value = ''
}

function normalizeErrorMessage(error: unknown, fallback: string) {
  const raw = error instanceof Error ? error.message : fallback
  const lower = raw.toLowerCase()

  if (lower.includes('no such column') || lower.includes('sql logic error')) {
    return '当前数据库结构版本过旧或初始化不完整，建议点击“返回重新初始化数据库”后重新初始化。'
  }
  if (lower.includes('database is locked')) {
    return '数据库当前正被占用，请稍后重试。'
  }
  if (lower.includes('unique') || lower.includes('已存在')) {
    return '数据已存在，请检查输入内容后重试。'
  }
  if (lower.includes('unauthorized') || lower.includes('请先登录') || lower.includes('登录已失效')) {
    return '当前登录状态已失效，请重新登录。'
  }

  return raw
}

function syncCronExprFromDraft() {
  reminderDraft.recurrenceExpr = cronPreview.value.trim()
}

function syncCronDraftFromExpr(expr: string) {
  const fields = expr.trim().split(/\s+/)
  if (fields.length !== 5) return
  ;[cronDraft.minute, cronDraft.hour, cronDraft.day, cronDraft.month, cronDraft.weekday] = fields
}

function customOffsetMinutes() {
  const raw = Number(customReminderDraft.value || 0)
  if (!Number.isFinite(raw) || raw <= 0) return 0
  if (customReminderDraft.unit === 'day') return Math.floor(raw * 1440)
  if (customReminderDraft.unit === 'hour') return Math.floor(raw * 60)
  return Math.floor(raw)
}

function customReminderLabel(offsetMin: number) {
  if (offsetMin % 1440 === 0) return `提前 ${offsetMin / 1440} 天`
  if (offsetMin % 60 === 0) return `提前 ${offsetMin / 60} 小时`
  return `提前 ${offsetMin} 分钟`
}

function addCustomReminderOption() {
  const offsetMin = customOffsetMinutes()
  if (offsetMin <= 0) {
    errorMessage.value = '自定义预提醒必须大于 0'
    return
  }
  const existed = reminderOptions.find((item) => item.offsetMin === offsetMin)
  if (existed) {
    existed.selected = true
    successMessage.value = '该预提醒已存在，已为你选中。'
    return
  }
  reminderOptions.push({
    key: `custom-${offsetMin}-${Date.now()}`,
    label: customReminderLabel(offsetMin),
    offsetMin,
    selected: true
  })
  successMessage.value = '已添加自定义预提醒。'
}

function loadUiSettings() {
  const raw = localStorage.getItem(SETTINGS_KEY)
  if (!raw) return
  try {
    Object.assign(uiSettings, JSON.parse(raw))
  } catch {
    return
  }
}

function saveComposerDraft() {
  const payload = {
    title: reminderDraft.title,
    content: reminderDraft.content,
    eventAt: reminderDraft.eventAt,
    recurrenceType: reminderDraft.recurrenceType,
    recurrenceExpr: reminderDraft.recurrenceExpr,
    reminderOffsets: reminderOptions.filter((item) => item.selected).map((item) => item.offsetMin),
    selectedGroupIds: selectedGroupIds.value
  }
  localStorage.setItem(COMPOSER_DRAFT_KEY, JSON.stringify(payload))
}

function restoreComposerDraft() {
  const raw = localStorage.getItem(COMPOSER_DRAFT_KEY)
  if (!raw) return
  try {
    const draft = JSON.parse(raw) as {
      title?: string
      content?: string
      eventAt?: string
      recurrenceType?: 'once' | 'daily' | 'workday' | 'cron'
      recurrenceExpr?: string
      reminderOffsets?: number[]
      selectedGroupIds?: number[]
    }
    if (draft.title) reminderDraft.title = draft.title
    if (draft.content) reminderDraft.content = draft.content
    if (draft.eventAt) reminderDraft.eventAt = draft.eventAt
    if (draft.recurrenceType) reminderDraft.recurrenceType = draft.recurrenceType
    reminderDraft.recurrenceExpr = draft.recurrenceExpr || ''
    if (Array.isArray(draft.reminderOffsets)) {
      reminderOptions.forEach((item) => {
        item.selected = draft.reminderOffsets?.includes(item.offsetMin) ?? false
      })
    }
    if (Array.isArray(draft.selectedGroupIds)) {
      selectedGroupIds.value = [...new Set(draft.selectedGroupIds)]
    }
  } catch {
    return
  }
}

function clearComposerDraft() {
  localStorage.removeItem(COMPOSER_DRAFT_KEY)
}

function saveUiSettings() {
  savingUi.value = true
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(uiSettings))
  successMessage.value = '界面设置已保存。'
  setTimeout(() => {
    savingUi.value = false
  }, 250)
}

function toggleGroup(id: number) {
  selectedGroupIds.value = selectedGroupIds.value.includes(id)
    ? selectedGroupIds.value.filter((item) => item !== id)
    : [...selectedGroupIds.value, id]
}

function openConfirm(message: string, title = '请确认') {
  return new Promise<boolean>((resolve) => {
    confirmState.title = title
    confirmState.message = message
    confirmState.visible = true
    confirmState.resolve = resolve
  })
}

function closeConfirm(confirmed: boolean) {
  if (confirmState.resolve) {
    confirmState.resolve(confirmed)
  }
  confirmState.visible = false
  confirmState.resolve = null
}

async function switchView(nextView: ViewMode) {
  if (nextView === currentView.value) return
  if (currentView.value === 'notify' && notifyEditorVisible.value && isGroupDirty.value) {
    const ok = await openConfirm('当前通知组有未保存修改，确定要离开并放弃这些修改吗？')
    if (!ok) return
  }
  if (nextView === 'list' && currentView.value !== 'list') {
    listFocus.value = 'pending'
  }
  if (nextView === 'users' && isAdmin.value) {
    await loadManagedUsers()
  }
  currentView.value = nextView
  if (nextView !== 'notify') {
    notifyEditorVisible.value = false
    resetGroupForm()
  }
}

function toApiDateTime(value: string) {
  return value ? `${value}:00+08:00` : ''
}

function toDateTimeLocal(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  const offsetMs = date.getTimezoneOffset() * 60 * 1000
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16)
}

async function loadData(options?: { silent?: boolean }) {
  const silent = options?.silent === true
  if (!silent) {
    loading.value = true
  }
  resetMessages()
  bootstrapError.value = ''
  try {
    let meUser: AuthUser | null = null
    if (hasToken()) {
      try {
        const me = await fetchMe()
        meUser = me.user
      } catch {
        clearToken()
      }
    }
    appState.value = await fetchBootstrap()
      if (appState.value) {
      const shouldShowLoginReadyHint =
        isRealInitializedAt(appState.value.database.initializedAt) &&
        appState.value.auth.adminExists &&
        !appState.value.auth.currentUser &&
        localStorage.getItem(LOGIN_READY_HINT_SEEN_KEY) !== '1'
      showLoginReadyHint.value = shouldShowLoginReadyHint
      if (shouldShowLoginReadyHint) {
        localStorage.setItem(LOGIN_READY_HINT_SEEN_KEY, '1')
      }

      if (!appState.value.auth.currentUser && meUser) {
        appState.value.auth.currentUser = meUser
      }
      dbForm.driver = isRealInitializedAt(appState.value.database.initializedAt) ? appState.value.database.selectedDriver : ''
      dbForm.sqlitePath = appState.value.database.sqlitePath || './data/waitwhat.sqlite'
      dbForm.pgHost = appState.value.database.pgHost || '127.0.0.1'
      dbForm.pgPort = appState.value.database.pgPort || 5432
      dbForm.pgDatabase = appState.value.database.pgDatabase || 'waitwhat'
      dbForm.pgUser = appState.value.database.pgUser || 'postgres'
      dbForm.pgSslMode = appState.value.database.pgSslMode || 'disable'
      mailForm.enabled = appState.value.mail.enabled
      mailForm.host = appState.value.mail.host || 'smtp.qq.com'
      mailForm.port = appState.value.mail.port || 587
      mailForm.username = appState.value.mail.username || ''
      mailForm.password = ''
      mailPasswordSaved.value = appState.value.mail.hasPassword
      mailForm.fromName = appState.value.mail.fromName || uiSettings.projectName
      mailForm.fromAddress = appState.value.mail.fromAddress || ''
      mailForm.useTls = appState.value.mail.useTls
      mailForm.useSsl = appState.value.mail.useSsl
      reminderDraft.testMailTo = ''
      selectedGroupIds.value = []
      loginPolicyForm.loginMaxFailed = appState.value.auth.loginMaxFailed || 5
      loginPolicyForm.loginWindowSec = appState.value.auth.loginWindowSec || 600
      if (currentUser.value) {
        uiSettings.displayName = currentUser.value.name || currentUser.value.username
      }
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : '加载失败'
    errorMessage.value = message
    bootstrapError.value = message
  } finally {
    if (!silent) {
      loading.value = false
    }
  }
}

function retryLoadData() {
  void loadData()
}

async function submitDatabaseConfig() {
  submitting.value = true
  resetMessages()
  try {
    if (!dbForm.driver) {
      throw new Error('请先选择 SQLite 或 PostgreSQL')
    }
    await initDatabase({ ...dbForm, driver: dbForm.driver as DatabaseDriver })
    successMessage.value = '数据库初始化完成，请继续设置管理员账号。'
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '保存失败')
  } finally {
    submitting.value = false
  }
}

async function restartInitialization() {
  resetMessages()
  try {
    await resetDatabase()
    clearToken()
    editingEventId.value = null
    dbForm.driver = ''
    dbForm.sqlitePath = './data/waitwhat.sqlite'
    dbForm.pgHost = '127.0.0.1'
    dbForm.pgPort = 5432
    dbForm.pgDatabase = 'waitwhat'
    dbForm.pgUser = 'postgres'
    dbForm.pgPassword = ''
    dbForm.pgSslMode = 'disable'
    localStorage.removeItem(LOGIN_READY_HINT_SEEN_KEY)
    showLoginReadyHint.value = false
    successMessage.value = '已返回数据库初始化，请重新配置。'
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '重置失败')
  }
}

async function submitAdminSetup() {
  authSubmitting.value = true
  resetMessages()
  try {
    const result = await setupAdmin({ ...adminForm })
    saveToken(result.token)
    successMessage.value = '管理员创建成功。'
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '管理员创建失败')
  } finally {
    authSubmitting.value = false
  }
}

async function submitAuth() {
  authSubmitting.value = true
  resetMessages()
  try {
    const result =
      authMode.value === 'login'
        ? await login({ username: authForm.username, password: authForm.password })
        : await register(authForm)
    saveToken(result.token)
    successMessage.value = authMode.value === 'login' ? '登录成功。' : '注册并登录成功。'
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '认证失败')
  } finally {
    authSubmitting.value = false
  }
}

async function handleLogout() {
  try {
    await apiLogout()
  } catch {
    // ignore
  }
  clearToken()
  await loadData()
  successMessage.value = '已退出登录。'
}

async function loadManagedUsers() {
  if (!isAdmin.value) {
    managedUsers.value = []
    return
  }
  const response = await adminListUsers()
  managedUsers.value = response.users
}

async function loadAuditLogs() {
  if (!isAdmin.value) {
    adminAuditLogs.value = []
    auditTotal.value = 0
    return
  }
  loadingAudit.value = true
  try {
    const offset = (auditPage.value - 1) * auditPageSize.value
    const response = await adminListAuditLogs(auditPageSize.value, offset)
    adminAuditLogs.value = response.items
    auditTotal.value = response.total
  } finally {
    loadingAudit.value = false
  }
}

function openUserEditor(user: AuthUser) {
  editingManagedUserId.value = user.id
  userEditorVisible.value = true
}

function closeUserEditor() {
  editingManagedUserId.value = 0
  userEditorVisible.value = false
}

async function submitLoginPolicy() {
  savingLoginPolicy.value = true
  resetMessages()
  try {
    await adminUpdateLoginPolicy({
      loginMaxFailed: loginPolicyForm.loginMaxFailed,
      loginWindowSec: loginPolicyForm.loginWindowSec
    })
    successMessage.value = '登录限流策略已更新。'
    await loadAuditLogs()
    await loadData({ silent: true })
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '登录限流策略更新失败')
  } finally {
    savingLoginPolicy.value = false
  }
}

async function submitResetUserPassword(user: AuthUser) {
  const password = (userPasswordDraft[user.id] || '').trim()
  if (!password) {
    errorMessage.value = '请输入新密码'
    return
  }
  resettingUserPasswordId.value = user.id
  resetMessages()
  try {
    await adminUpdateUserPassword(user.id, password)
    userPasswordDraft[user.id] = ''
    successMessage.value = `用户「${user.username}」密码已更新。`
    await loadAuditLogs()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '修改用户密码失败')
  } finally {
    resettingUserPasswordId.value = 0
  }
}

async function removeUser(user: AuthUser) {
  const confirmed = await openConfirm(`确定删除用户「${user.username}」吗？该用户的事件、通知组和配置会一并删除。`, '删除用户')
  if (!confirmed) return
  deletingUser.value = true
  resetMessages()
  try {
    await adminDeleteUser(user.id)
    successMessage.value = `用户「${user.username}」已删除。`
    await loadData({ silent: true })
    await loadManagedUsers()
    await loadAuditLogs()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '删除用户失败')
  } finally {
    deletingUser.value = false
  }
}

async function submitEvent() {
  creating.value = true
  resetMessages()
  try {
    const reminderPoints = reminderOptions.filter((item) => item.selected).map((item, index) => ({
      id: Date.now() + index,
      label: item.label,
      offsetMin: item.offsetMin
    }))
    reminderPoints.push({ id: Date.now() + 500, label: '到点提醒', offsetMin: 0 })
    if (reminderPoints.length === 0) throw new Error('请至少选择一个预提醒时间')
    if (selectedGroupIds.value.length === 0) throw new Error('请至少选择一个通知组')
    if (reminderDraft.recurrenceType === 'cron' && !reminderDraft.recurrenceExpr.trim()) {
      throw new Error('Cron 周期模式下请填写表达式')
    }

    const payload = {
      userId: currentUser.value?.id ?? 0,
      title: reminderDraft.title,
      content: reminderDraft.content,
      eventAt: reminderDraft.recurrenceType === 'cron' ? '' : toApiDateTime(reminderDraft.eventAt),
      reminderEnabled: true,
      recurrenceType: reminderDraft.recurrenceType,
      recurrenceExpr: reminderDraft.recurrenceExpr.trim(),
      reminderPoints,
      boundChannelIds: [],
      boundGroupIds: selectedGroupIds.value
    }

    if (editingEventId.value) {
      await updateEvent(editingEventId.value, payload)
      successMessage.value = '事件已更新。'
    } else {
      await createEvent(payload)
      successMessage.value = '事件已经写入数据库。'
      clearComposerDraft()
    }
    editingEventId.value = null
    if (editReturnContext.value) {
      currentView.value = 'list'
      listFocus.value = editReturnContext.value.tab
      if (editReturnContext.value.tab === 'pending') {
        pendingPage.value = Math.max(1, editReturnContext.value.page)
      } else {
        expiredPage.value = Math.max(1, editReturnContext.value.page)
      }
      editReturnContext.value = null
    } else {
      switchView('list')
    }
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '事件创建失败')
  } finally {
    creating.value = false
  }
}

function beginEdit(event: AppState['events'][number]) {
  if (currentView.value === 'list') {
    editReturnContext.value = {
      tab: listFocus.value,
      page: listFocus.value === 'pending' ? pendingPage.value : expiredPage.value
    }
  } else {
    editReturnContext.value = null
  }
  switchView('composer')
  editingEventId.value = event.id
  reminderDraft.title = event.title
  reminderDraft.content = event.content
  reminderDraft.eventAt = toDateTimeLocal(event.eventAt)
  reminderDraft.recurrenceType = event.recurrenceType || 'once'
  reminderDraft.recurrenceExpr = event.recurrenceExpr || ''
  if (reminderDraft.recurrenceType === 'cron' && reminderDraft.recurrenceExpr) {
    syncCronDraftFromExpr(reminderDraft.recurrenceExpr)
  }
  selectedGroupIds.value = [...(event.boundGroupIds ?? [])]
  reminderOptions.forEach((option) => {
    option.selected = event.reminderPoints.some((point) => point.offsetMin === option.offsetMin)
  })
}

function cancelEdit() {
  editingEventId.value = null
  if (editReturnContext.value) {
    currentView.value = 'list'
    listFocus.value = editReturnContext.value.tab
    if (editReturnContext.value.tab === 'pending') {
      pendingPage.value = Math.max(1, editReturnContext.value.page)
    } else {
      expiredPage.value = Math.max(1, editReturnContext.value.page)
    }
    editReturnContext.value = null
  }
}

function toggleEventSelection(eventId: number) {
  selectedEventIds.value = selectedEventIds.value.includes(eventId)
    ? selectedEventIds.value.filter((id) => id !== eventId)
    : [...selectedEventIds.value, eventId]
}

function toggleCurrentPageSelection() {
  const pageIDs = currentPageEvents.value.map((item) => item.id)
  if (pageIDs.length === 0) return
  if (allCurrentPageSelected.value) {
    selectedEventIds.value = selectedEventIds.value.filter((id) => !pageIDs.includes(id))
  } else {
    const merged = new Set([...selectedEventIds.value, ...pageIDs])
    selectedEventIds.value = Array.from(merged)
  }
}

async function batchDeleteSelectedEvents() {
  if (selectedEventIds.value.length === 0) return
  const ok = await openConfirm(`确定删除选中的 ${selectedEventIds.value.length} 个事件吗？`, '批量删除')
  if (!ok) return
  resetMessages()
  const ids = [...selectedEventIds.value]
  try {
    for (const id of ids) {
      await deleteEvent(id)
    }
    selectedEventIds.value = []
    successMessage.value = `已删除 ${ids.length} 个事件。`
    await loadData({ silent: true })
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '批量删除失败')
  }
}

async function batchUpdateReminderEnabled(enabled: boolean) {
  if (selectedEventIds.value.length === 0) return
  resetMessages()
  try {
    const eventMap = new Map(events.value.map((item) => [item.id, item]))
    let count = 0
    for (const id of selectedEventIds.value) {
      const event = eventMap.get(id)
      if (!event) continue
      await updateEvent(id, {
        userId: event.userId,
        title: event.title,
        content: event.content,
        eventAt: event.eventAt,
        reminderEnabled: enabled,
        recurrenceType: event.recurrenceType,
        recurrenceExpr: event.recurrenceExpr || '',
        reminderPoints: event.reminderPoints,
        boundChannelIds: event.boundChannelIds,
        boundGroupIds: event.boundGroupIds
      })
      count++
    }
    selectedEventIds.value = []
    successMessage.value = enabled ? `已启用 ${count} 个事件提醒。` : `已停用 ${count} 个事件提醒。`
    await loadData({ silent: true })
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '批量更新提醒失败')
  }
}

async function removeEvent(eventId: number) {
  resetMessages()
  try {
    await deleteEvent(eventId)
    successMessage.value = '事件已删除。'
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '删除失败')
  }
}

async function submitMailConfig() {
  savingMail.value = true
  resetMessages()
  try {
    await saveMailConfig({ ...mailForm })
    mailPasswordSaved.value = true
    mailForm.password = ''
    successMessage.value = 'SMTP 配置已保存。'
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, 'SMTP 配置保存失败')
  } finally {
    savingMail.value = false
  }
}

async function testMailConfig() {
  testingMail.value = true
  resetMessages()
  try {
    await sendTestMail(reminderDraft.testMailTo)
    successMessage.value = '测试邮件已发送，请检查收件箱。'
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '测试邮件发送失败')
  } finally {
    testingMail.value = false
  }
}

async function runMailDiagnosis() {
  diagnosingMail.value = true
  resetMessages()
  try {
    const response = await diagnoseMail(mailForm.host)
    mailDiagnosis.value = response.result
    successMessage.value = 'SMTP 连接诊断已完成。'
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, 'SMTP 连接诊断失败')
  } finally {
    diagnosingMail.value = false
  }
}

async function runDispatch() {
  dispatching.value = true
  resetMessages()
  try {
    const response = await dispatchReminders()
    successMessage.value = `扫描完成：触发 ${response.result.triggered}，成功 ${response.result.sent}，重试中 ${response.result.retried}，失败 ${response.result.failed}，跳过 ${response.result.skipped}。`
    await loadData({ silent: true })
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '提醒扫描失败')
  } finally {
    dispatching.value = false
  }
}

function resetGroupForm() {
  groupForm.id = 0
  groupForm.name = ''
  groupForm.enabled = true
  groupForm.members = [{ type: 'email', label: '默认邮箱', target: '', secret: '', keyword: '提醒', useSign: false, enabled: true }]
}

function addGroupMember() {
  groupForm.members.push({ type: 'email', label: '', target: '', secret: '', keyword: '提醒', useSign: false, enabled: true })
}

function serializeGroupDraft() {
  return JSON.stringify({
    id: groupForm.id,
    name: groupForm.name.trim(),
    enabled: groupForm.enabled,
    members: groupForm.members.map((member) => ({
      type: member.type,
      label: member.label.trim(),
      target: member.target.trim(),
      secret: member.secret.trim(),
      keyword: member.keyword.trim(),
      useSign: member.useSign,
      enabled: member.enabled
    }))
  })
}

function createGroup() {
  resetGroupForm()
  notifyEditorVisible.value = true
  groupDraftSnapshot.value = serializeGroupDraft()
}

function editGroup(group: NotificationGroup) {
  groupForm.id = group.id
  groupForm.name = group.name
  groupForm.enabled = group.enabled
  groupForm.members = group.members.map((m) => ({
    type: m.type,
    label: m.label,
    target: m.target,
    secret: '',
    keyword: m.keyword || '提醒',
    useSign: m.useSign,
    enabled: m.enabled
  }))
  notifyEditorVisible.value = true
  groupDraftSnapshot.value = serializeGroupDraft()
}

function routeHash() {
  return window.location.hash || '#/composer'
}

function parseHash(hash: string) {
  const raw = hash.startsWith('#') ? hash.slice(1) : hash
  const [pathPart, queryPart = ''] = raw.split('?')
  const params = new URLSearchParams(queryPart)
  return {
    path: pathPart || '/composer',
    params
  }
}

function setHash(hash: string, replace = false) {
  if (replace) {
    window.history.replaceState(null, '', hash)
    return
  }
  window.history.pushState(null, '', hash)
}

function syncRouteFromState(replace = false) {
  if (applyingUrlState.value) return
  let hash = '#/composer'
  if (currentView.value === 'list') {
    hash = `#/list?tab=${listFocus.value}`
  } else if (currentView.value === 'users') {
    hash = '#/users'
  } else if (currentView.value === 'settings') {
    hash = '#/settings'
  } else if (currentView.value === 'notify') {
    if (notifyEditorVisible.value) {
      hash = groupForm.id ? `#/notify/group/${groupForm.id}` : '#/notify/group/new'
    } else {
      hash = '#/notify'
    }
  }
  if (routeHash() !== hash) {
    setHash(hash, replace)
  }
}

async function applyRouteToState() {
  const { path, params } = parseHash(routeHash())

  const leavingDirtyNotify =
    currentView.value === 'notify' &&
    notifyEditorVisible.value &&
    isGroupDirty.value &&
    !path.startsWith('/notify/group/')
  if (leavingDirtyNotify) {
    const ok = await openConfirm('当前通知组有未保存修改，确定要离开并放弃这些修改吗？')
    if (!ok) {
      syncRouteFromState(true)
      return
    }
  }

  applyingUrlState.value = true
  if (path === '/composer') {
    currentView.value = 'composer'
    notifyEditorVisible.value = false
    resetGroupForm()
  } else if (path === '/list') {
    currentView.value = 'list'
    const tab = params.get('tab')
    listFocus.value = tab === 'expired' ? 'expired' : 'pending'
    notifyEditorVisible.value = false
    resetGroupForm()
  } else if (path === '/settings') {
    currentView.value = 'settings'
    notifyEditorVisible.value = false
    resetGroupForm()
  } else if (path === '/users') {
    currentView.value = 'users'
    notifyEditorVisible.value = false
    resetGroupForm()
    if (isAdmin.value) {
      await loadManagedUsers()
    } else {
      currentView.value = 'composer'
      syncRouteFromState(true)
    }
  } else if (path === '/notify') {
    currentView.value = 'notify'
    notifyEditorVisible.value = false
    resetGroupForm()
  } else if (path === '/notify/group/new') {
    currentView.value = 'notify'
    createGroup()
  } else if (path.startsWith('/notify/group/')) {
    currentView.value = 'notify'
    const idText = path.replace('/notify/group/', '')
    const groupID = Number(idText)
    const target = Number.isFinite(groupID) ? notifyGroups.value.find((group) => group.id === groupID) : undefined
    if (target) {
      editGroup(target)
    } else {
      notifyEditorVisible.value = false
      resetGroupForm()
    }
  } else {
    currentView.value = 'composer'
    notifyEditorVisible.value = false
    resetGroupForm()
    syncRouteFromState(true)
  }
  applyingUrlState.value = false
}

async function closeGroupEditor() {
  if (isGroupDirty.value) {
    const ok = await openConfirm('当前通知组有未保存修改，是否放弃变更并退出？')
    if (!ok) return
  }
  notifyEditorVisible.value = false
  resetGroupForm()
}

async function submitNotifyGroup() {
  savingGroup.value = true
  resetMessages()
  try {
    await saveNotifyGroup({ ...groupForm })
    successMessage.value = '通知组已保存。'
    notifyEditorVisible.value = false
    resetGroupForm()
    await loadData({ silent: true })
    currentView.value = 'notify'
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '通知组保存失败')
  } finally {
    savingGroup.value = false
  }
}

async function removeGroup(id: number) {
  resetMessages()
  try {
    const target = notifyGroups.value.find((group) => group.id === id)
    const confirmed = await openConfirm(`确定删除通知组「${target?.name ?? id}」吗？`, '删除通知组')
    if (!confirmed) return
    await deleteNotifyGroup(id)
    successMessage.value = '通知组已删除。'
    if (groupForm.id === id && notifyEditorVisible.value) {
      notifyEditorVisible.value = false
      resetGroupForm()
    }
    await loadData({ silent: true })
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '通知组删除失败')
  }
}

async function testGroupMember(member: { type: 'email' | 'dingtalk_webhook'; target: string; secret: string; keyword: string; useSign: boolean }, idx: number) {
  resetMessages()
  const key = `${member.type}-${idx}`
  testingMemberKey.value = key
  memberTestStatus[key] = { state: 'sending', message: '发送中...' }
  try {
    if (member.type === 'email') {
      await sendTestMail(member.target)
      successMessage.value = '邮箱测试消息已发送，请检查收件箱（含“测试”内容）。'
      memberTestStatus[key] = { state: 'success', message: '邮箱测试发送成功' }
    } else {
      await sendTestDingTalk({
        webhook: member.target,
        secret: member.secret,
        keyword: member.keyword,
        useSign: member.useSign
      })
      successMessage.value = '钉钉测试消息已发送（含“测试”内容）。'
      memberTestStatus[key] = { state: 'success', message: '钉钉测试发送成功' }
    }
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '测试消息发送失败')
    memberTestStatus[key] = { state: 'error', message: errorMessage.value || '发送失败' }
  } finally {
    testingMemberKey.value = ''
  }
}

onMounted(async () => {
  loadUiSettings()
  await loadData()
  restoreComposerDraft()
  await loadManagedUsers()
  await loadAuditLogs()
  applyRouteToState()
  window.addEventListener('hashchange', applyRouteToState)
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', applyRouteToState)
})

watch([currentView, notifyEditorVisible, () => groupForm.id], () => {
  syncRouteFromState()
})

watch(listFocus, () => {
  if (currentView.value === 'list') {
    syncRouteFromState()
  }
})

watch(listPageSize, () => {
  pendingPage.value = 1
  expiredPage.value = 1
  if (editReturnContext.value) {
    editReturnContext.value.page = 1
  }
})

watch([listQuery, listSort], () => {
  pendingPage.value = 1
  expiredPage.value = 1
  selectedEventIds.value = []
})

watch(userPageSize, () => {
  userPage.value = 1
})

watch(auditPageSize, () => {
  auditPage.value = 1
})

watch(pendingTotalPages, (total) => {
  if (pendingPage.value > total) pendingPage.value = total
})

watch(expiredTotalPages, (total) => {
  if (expiredPage.value > total) expiredPage.value = total
})

watch(userTotalPages, (total) => {
  if (userPage.value > total) userPage.value = total
})

watch(auditTotalPages, (total) => {
  if (auditPage.value > total) auditPage.value = total
})

watch(listFocus, () => {
  selectedEventIds.value = []
})

watch(
  () => reminderDraft.recurrenceType,
  (type) => {
    if (type === 'cron') {
      reminderDraft.eventAt = nowDateTimeLocal()
      if (!reminderDraft.recurrenceExpr.trim()) {
        syncCronExprFromDraft()
      } else {
        syncCronDraftFromExpr(reminderDraft.recurrenceExpr)
      }
      return
    }
    reminderDraft.recurrenceExpr = ''
  }
)

watch(
  () => [cronDraft.minute, cronDraft.hour, cronDraft.day, cronDraft.month, cronDraft.weekday],
  () => {
    if (reminderDraft.recurrenceType === 'cron') {
      syncCronExprFromDraft()
    }
  }
)

watch([currentView, isAdmin], async ([view, admin]) => {
  if ((view === 'settings' || view === 'users') && admin) {
    await loadManagedUsers()
    await loadAuditLogs()
  }
})

watch([auditPage, auditPageSize, currentView], async () => {
  if (currentView.value === 'users' && isAdmin.value) {
    await loadAuditLogs()
  }
})

watch(
  () => ({
    title: reminderDraft.title,
    content: reminderDraft.content,
    eventAt: reminderDraft.eventAt,
    recurrenceType: reminderDraft.recurrenceType,
    recurrenceExpr: reminderDraft.recurrenceExpr,
    offsets: reminderOptions.map((item) => ({ offset: item.offsetMin, selected: item.selected })),
    groupIDs: [...selectedGroupIds.value]
  }),
  () => {
    if (!loggedIn.value || editingEventId.value) return
    saveComposerDraft()
  },
  { deep: true }
)
</script>

<template>
  <div class="app-frame">
    <div class="ambient ambient-one"></div>
    <div class="ambient ambient-two"></div>

    <div v-if="loading" class="center-shell">
      <div class="auth-card">
        <p class="eyebrow">WaitWhat</p>
        <h1>正在准备你的提醒工作台</h1>
        <p class="muted-copy">加载数据库状态、用户体系和提醒数据中...</p>
      </div>
    </div>

    <div v-else-if="backendUnavailable" class="center-shell">
      <div class="auth-card">
        <p class="eyebrow">Backend</p>
        <h1>后端服务暂不可用</h1>
        <p class="muted-copy">当前无法连接到 API（通常是后端未启动或端口未就绪），所以不会进入数据库初始化流程。</p>
        <div class="form-actions top-gap">
          <button class="primary-btn" @click="retryLoadData">重新连接</button>
        </div>
        <p class="status-text error" v-if="bootstrapError">{{ bootstrapError }}</p>
      </div>
    </div>

    <div v-else-if="!databaseConfigured" class="center-shell">
      <div class="setup-shell">
        <section class="setup-brand">
          <p class="eyebrow">First Step</p>
          <h1>先选择你的数据库模式</h1>
          <p class="muted-copy">首次访问先完成数据库初始化。完成后系统会要求设置管理员账号。</p>
        </section>
        <section class="setup-panel glass-panel">
          <div class="panel-head">
            <div>
              <p class="section-label">数据库初始化</p>
              <h2>选择存储方式</h2>
            </div>
          </div>
          <div class="pill-switch">
            <button :class="['pill-btn', dbForm.driver === 'sqlite' && 'active']" @click="dbForm.driver = 'sqlite'">SQLite</button>
            <button :class="['pill-btn', dbForm.driver === 'postgres' && 'active']" @click="dbForm.driver = 'postgres'">PostgreSQL</button>
          </div>
          <p class="muted-copy" v-if="!dbForm.driver">先选择数据库类型，然后再填写对应配置。</p>
          <div class="form-grid" v-if="dbForm.driver === 'sqlite'">
            <label class="field field-full">
              <span>SQLite 文件路径</span>
              <input v-model="dbForm.sqlitePath" />
            </label>
          </div>
          <div class="form-grid" v-else>
            <label class="field"><span>主机</span><input v-model="dbForm.pgHost" /></label>
            <label class="field"><span>端口</span><input v-model.number="dbForm.pgPort" type="number" /></label>
            <label class="field"><span>数据库名</span><input v-model="dbForm.pgDatabase" /></label>
            <label class="field"><span>用户名</span><input v-model="dbForm.pgUser" /></label>
            <label class="field field-full"><span>密码</span><input v-model="dbForm.pgPassword" type="password" /></label>
          </div>
          <div class="form-actions">
            <button class="primary-btn" :disabled="submitting" @click="submitDatabaseConfig">{{ submitting ? '保存中...' : '保存数据库配置' }}</button>
          </div>
          <p class="status-text success" v-if="successMessage">{{ successMessage }}</p>
          <p class="status-text error" v-if="errorMessage">{{ errorMessage }}</p>
        </section>
      </div>
    </div>

    <div v-else-if="!adminConfigured" class="center-shell">
      <div class="auth-card">
        <p class="eyebrow">Admin Setup</p>
        <h1>创建管理员账号</h1>
        <p class="muted-copy">数据库已经初始化完成，现在设置系统的第一个管理员用户。</p>
        <div class="form-grid">
          <label class="field"><span>管理员用户名</span><input v-model="adminForm.username" /></label>
          <label class="field"><span>管理员昵称</span><input v-model="adminForm.name" /></label>
          <label class="field"><span>邮箱</span><input v-model="adminForm.email" /></label>
          <label class="field"><span>密码</span><input v-model="adminForm.password" type="password" /></label>
        </div>
        <div class="form-actions">
          <button class="primary-btn" :disabled="authSubmitting" @click="submitAdminSetup">{{ authSubmitting ? '创建中...' : '创建管理员并进入系统' }}</button>
          <button v-if="showResetInit" class="danger-btn" @click="restartInitialization">返回重新初始化数据库</button>
        </div>
        <p class="status-text success" v-if="successMessage">{{ successMessage }}</p>
        <p class="status-text error" v-if="errorMessage">{{ errorMessage }}</p>
      </div>
    </div>

    <div v-else-if="!loggedIn" class="center-shell">
      <div class="auth-card login-modal">
        <div class="login-head">
          <p class="eyebrow">Welcome</p>
          <h2>欢迎回来</h2>
          <p class="muted-copy">{{ showLoginReadyHint ? '管理员已设置完成，请登录继续。' : '请登录你的账号' }}</p>
        </div>
        <div class="pill-switch login-switch">
          <button :class="['pill-btn', authMode === 'login' && 'active']" @click="authMode = 'login'">登录</button>
          <button :class="['pill-btn', authMode === 'register' && 'active']" @click="authMode = 'register'">注册</button>
        </div>
        <div class="login-form">
          <input v-model="authForm.username" placeholder="用户名" />
          <input v-model="authForm.password" type="password" placeholder="密码" />
          <input v-if="authMode === 'register'" v-model="authForm.name" placeholder="昵称（注册必填）" />
          <input v-if="authMode === 'register'" v-model="authForm.email" placeholder="邮箱（注册必填）" />
        </div>
        <div class="form-actions login-actions">
          <button class="primary-btn login-submit" :disabled="authSubmitting" @click="submitAuth">{{ authSubmitting ? '提交中...' : authMode === 'login' ? '立即登录' : '注册并登录' }}</button>
        </div>
        <p class="login-footnote" v-if="authMode === 'login'">忘记密码功能暂未开放</p>
        <p class="status-text success" v-if="successMessage">{{ successMessage }}</p>
        <p class="status-text error" v-if="errorMessage">{{ errorMessage }}</p>
      </div>
    </div>

    <div v-else class="workspace-shell">
      <header class="topbar glass-panel">
        <div class="brand-box">
          <div class="brand-avatar">{{ uiSettings.projectName.slice(0, 1) }}</div>
          <div>
            <strong>{{ uiSettings.projectName }}</strong>
            <p>{{ uiSettings.slogan }}</p>
          </div>
        </div>
        <nav class="nav-pill">
          <button :class="['nav-btn', currentView === 'composer' && 'active']" @click="switchView('composer')">创建备忘录</button>
          <button :class="['nav-btn', currentView === 'list' && 'active']" @click="switchView('list')">备忘录列表</button>
          <button :class="['nav-btn', currentView === 'notify' && 'active']" @click="switchView('notify')">通知相关</button>
          <button v-if="isAdmin" :class="['nav-btn', currentView === 'users' && 'active']" @click="switchView('users')">用户管理</button>
          <button :class="['nav-btn', currentView === 'settings' && 'active']" @click="switchView('settings')">设置</button>
        </nav>
        <div class="account-box">
          <button class="icon-btn" @click="switchView('settings')">设置</button>
          <button class="icon-btn" @click="handleLogout">登出</button>
        </div>
      </header>

      <div class="status-banner" v-if="successMessage || errorMessage">
        <p class="status-text success" v-if="successMessage">{{ successMessage }}</p>
        <p class="status-text error" v-if="errorMessage">{{ errorMessage }}</p>
      </div>

      <section class="workspace-grid">
        <aside class="profile-card glass-panel">
          <div class="profile-avatar">{{ (currentUser?.name || currentUser?.username || 'U').slice(0, 1) }}</div>
          <div>
            <h2>{{ currentUser?.name || currentUser?.username }}</h2>
            <p>{{ currentUser?.role === 'admin' ? '管理员账号' : '普通用户' }}</p>
          </div>
          <div class="stat-stack">
            <div class="stat-item"><strong>{{ summaryStats.total }}</strong><span>事件总数</span></div>
            <div class="stat-item"><strong>{{ summaryStats.pending }}</strong><span>待处理任务</span></div>
            <div class="stat-item"><strong>{{ summaryStats.success }}</strong><span>成功通知</span></div>
          </div>
        </aside>

        <main class="content-column">
          <section v-if="currentView === 'composer'" class="content-panel glass-panel">
            <div class="panel-head"><div><p class="section-label">Memo Composer</p><h2>创建一个新的提醒事件</h2></div></div>
            <div class="feature-grid">
              <div class="editor-card">
                <label class="field"><span>标题</span><input v-model="reminderDraft.title" /></label>
                <label class="field"><span>内容</span><textarea v-model="reminderDraft.content" rows="5"></textarea></label>
                <label class="field">
                  <span>事件时间 <small v-if="reminderDraft.recurrenceType === 'cron'" class="field-lock-tag">Cron 中不可用</small></span>
                  <input
                    v-model="reminderDraft.eventAt"
                    type="datetime-local"
                    :disabled="reminderDraft.recurrenceType === 'cron'"
                    :class="{ 'is-disabled': reminderDraft.recurrenceType === 'cron' }"
                  />
                  <small v-if="reminderDraft.recurrenceType === 'cron'" class="field-hint">Cron 模式不需要手动设置事件时间。</small>
                </label>
                <div class="subsection">
                  <span class="subsection-title">预提醒时间</span>
                  <div class="selectable-grid">
                    <button v-for="option in reminderOptions" :key="option.key" type="button" :class="['select-chip', option.selected && 'active']" @click="option.selected = !option.selected">{{ option.label }}</button>
                  </div>
                  <div class="custom-reminder-tools">
                    <label class="field compact-field">
                      <span>自定义预提醒</span>
                      <input v-model.number="customReminderDraft.value" type="number" min="1" placeholder="例如 45" />
                    </label>
                    <label class="field compact-field">
                      <span>单位</span>
                      <select v-model="customReminderDraft.unit">
                        <option value="minute">分钟</option>
                        <option value="hour">小时</option>
                        <option value="day">天</option>
                      </select>
                    </label>
                    <button type="button" class="secondary-btn add-reminder-btn" @click="addCustomReminderOption">添加预提醒</button>
                  </div>
                </div>
                <div class="subsection">
                  <span class="subsection-title">提醒周期</span>
                  <div class="custom-reminder-row">
                    <label class="field compact-field">
                      <span>周期类型</span>
                      <select v-model="reminderDraft.recurrenceType">
                        <option value="once">一次性</option>
                        <option value="daily">每天</option>
                        <option value="workday">工作日</option>
                        <option value="cron">Cron 自定义</option>
                      </select>
                    </label>
                    <div class="cron-editor" v-if="reminderDraft.recurrenceType === 'cron'">
                      <div class="cron-grid">
                        <label class="field compact-field">
                          <span>分</span>
                          <input v-model="cronDraft.minute" placeholder="0" />
                        </label>
                        <label class="field compact-field">
                          <span>时</span>
                          <input v-model="cronDraft.hour" placeholder="9" />
                        </label>
                        <label class="field compact-field">
                          <span>日</span>
                          <input v-model="cronDraft.day" placeholder="*" />
                        </label>
                        <label class="field compact-field">
                          <span>月</span>
                          <input v-model="cronDraft.month" placeholder="*" />
                        </label>
                        <label class="field compact-field">
                          <span>周</span>
                          <input v-model="cronDraft.weekday" placeholder="1-5" />
                        </label>
                      </div>
                      <small class="field-hint">支持 `*`、`*/n`、区间（如 `1-5`）、枚举（如 `1,3,5`）。格式：分 时 日 月 周。</small>
                    </div>
                  </div>
                </div>
                <div class="subsection group-picker">
                  <span class="subsection-title">提醒对象（通知组）</span>
                  <div class="selectable-grid">
                    <button
                      v-for="group in notifyGroups"
                      :key="group.id"
                      type="button"
                      :class="['select-chip', selectedGroupIds.includes(group.id) && 'active', !group.enabled && 'disabled']"
                      @click="group.enabled && toggleGroup(group.id)"
                    >{{ group.name }}</button>
                  </div>
                </div>
                <div class="form-actions composer-actions">
                  <button class="primary-btn" :disabled="creating" @click="submitEvent">{{ creating ? '处理中...' : editingEventId ? '保存修改' : '创建这个事件' }}</button>
                  <button v-if="editingEventId" class="secondary-btn" @click="cancelEdit">取消编辑</button>
                </div>
              </div>

                <div class="preview-board">
                <div class="preview-summary"><p class="section-label">Preview</p><h3>{{ reminderDraft.title }}</h3><p>{{ reminderDraft.content }}</p></div>
                <div class="info-card"><span>事件时间</span><strong>{{ composerEventAtText }}</strong></div>
                <div class="info-card"><span>提醒周期</span><strong>{{ recurrenceSummary }}</strong></div>
                <div class="subsection"><span class="subsection-title">本次将生成的提醒点</span><div class="tag-row"><span class="mini-tag" v-for="point in selectedReminderSummary" :key="point">{{ point }}</span></div></div>
                <div class="subsection"><span class="subsection-title">本次选中的通知组</span><div class="tag-row"><span class="mini-tag" v-for="group in selectedGroups" :key="group.id">{{ group.name }}</span></div></div>
              </div>
            </div>
          </section>

          <section v-else-if="currentView === 'list'" class="content-panel glass-panel">
            <div class="panel-head align-center"><div><p class="section-label">Timeline</p><h2>备忘录列表</h2></div><button class="secondary-btn" :disabled="dispatching" @click="runDispatch">{{ dispatching ? '扫描中...' : '立即扫描提醒' }}</button></div>
            <div class="pill-switch list-switch">
              <button :class="['pill-btn', listFocus === 'pending' && 'active']" @click="listFocus = 'pending'">待提醒</button>
              <button :class="['pill-btn', listFocus === 'expired' && 'active']" @click="listFocus = 'expired'">已过期</button>
            </div>
            <div class="list-tools">
              <input v-model="listQuery" class="search-input" placeholder="搜索标题或内容" />
              <select v-model="listSort" class="list-sort-select">
                <option value="time_asc">按时间（近 → 远）</option>
                <option value="time_desc">按时间（远 → 近）</option>
                <option value="title_asc">按标题（A → Z）</option>
              </select>
            </div>
            <div class="batch-actions">
              <label class="checkbox-line"><input type="checkbox" :checked="allCurrentPageSelected" @change="toggleCurrentPageSelection" /><span>本页全选</span></label>
              <span class="batch-count">已选 {{ selectedEventIds.length }} 项</span>
              <button class="secondary-btn" :disabled="selectedEventIds.length === 0" @click="batchUpdateReminderEnabled(true)">批量启用提醒</button>
              <button class="secondary-btn" :disabled="selectedEventIds.length === 0" @click="batchUpdateReminderEnabled(false)">批量停用提醒</button>
              <button class="danger-btn" :disabled="selectedEventIds.length === 0" @click="batchDeleteSelectedEvents">批量删除</button>
            </div>
            <div class="memo-lanes top-gap">
              <section v-if="listFocus === 'pending'" :class="['subpanel', 'memo-lane', 'active-lane']">
                <div class="lane-head">
                  <h3>待提醒</h3>
                  <span class="lane-count">{{ filteredPendingEvents.length }}</span>
                </div>
                <div v-if="filteredPendingEvents.length === 0" class="lane-empty">暂无待提醒备忘录。</div>
                <div v-else class="memo-list-scroll">
                  <div class="memo-card-grid">
                  <article class="memo-mini-card" v-for="event in pagedPendingEvents" :key="event.id" @click="beginEdit(event)">
                    <label class="checkbox-line mini-select" @click.stop>
                      <input type="checkbox" :checked="selectedEventIds.includes(event.id)" @change="toggleEventSelection(event.id)" />
                      <span>选择</span>
                    </label>
                    <div class="memo-header compact">
                      <h3>{{ event.title }}</h3>
                    </div>
                    <div class="mini-meta">提醒时间：{{ new Date(event.eventAt).toLocaleString() }}</div>
                    <div class="form-actions top-gap">
                      <button class="secondary-btn" @click.stop="beginEdit(event)">编辑</button>
                      <button class="danger-btn" @click.stop="removeEvent(event.id)">删除</button>
                    </div>
                  </article>
                  </div>
                </div>
                <div v-if="filteredPendingEvents.length > 0" class="pager-row">
                  <div class="pager-left">
                    <span>每页显示</span>
                    <select v-model.number="listPageSize">
                      <option :value="12">12</option>
                      <option :value="30">30</option>
                      <option :value="50">50</option>
                      <option :value="100">100</option>
                    </select>
                    <span>条</span>
                  </div>
                  <div class="pager-right">
                    <button class="secondary-btn" :disabled="pendingPage <= 1" @click="pendingPage -= 1">上一页</button>
                    <span>{{ pendingPage }} / {{ pendingTotalPages }}</span>
                    <button class="secondary-btn" :disabled="pendingPage >= pendingTotalPages" @click="pendingPage += 1">下一页</button>
                  </div>
                </div>
              </section>
              <section v-else :class="['subpanel', 'memo-lane', 'active-lane']">
                <div class="lane-head">
                  <h3>已过期</h3>
                  <span class="lane-count">{{ filteredExpiredEvents.length }}</span>
                </div>
                <div v-if="filteredExpiredEvents.length === 0" class="lane-empty">暂无过期备忘录。</div>
                <div v-else class="memo-list-scroll">
                  <div class="memo-card-grid">
                  <article class="memo-mini-card" v-for="event in pagedExpiredEvents" :key="event.id" @click="beginEdit(event)">
                    <label class="checkbox-line mini-select" @click.stop>
                      <input type="checkbox" :checked="selectedEventIds.includes(event.id)" @change="toggleEventSelection(event.id)" />
                      <span>选择</span>
                    </label>
                    <div class="memo-header compact">
                      <h3>{{ event.title }}</h3>
                    </div>
                    <div class="mini-meta">提醒时间：{{ new Date(event.eventAt).toLocaleString() }}</div>
                    <div class="form-actions top-gap">
                      <button class="secondary-btn" @click.stop="beginEdit(event)">编辑</button>
                      <button class="danger-btn" @click.stop="removeEvent(event.id)">删除</button>
                    </div>
                  </article>
                  </div>
                </div>
                <div v-if="filteredExpiredEvents.length > 0" class="pager-row">
                  <div class="pager-left">
                    <span>每页显示</span>
                    <select v-model.number="listPageSize">
                      <option :value="12">12</option>
                      <option :value="30">30</option>
                      <option :value="50">50</option>
                      <option :value="100">100</option>
                    </select>
                    <span>条</span>
                  </div>
                  <div class="pager-right">
                    <button class="secondary-btn" :disabled="expiredPage <= 1" @click="expiredPage -= 1">上一页</button>
                    <span>{{ expiredPage }} / {{ expiredTotalPages }}</span>
                    <button class="secondary-btn" :disabled="expiredPage >= expiredTotalPages" @click="expiredPage += 1">下一页</button>
                  </div>
                </div>
              </section>
            </div>
            <div class="dual-grid">
              <section class="subpanel log-scroll-panel"><div class="panel-head"><div><p class="section-label">Tasks</p><h3>提醒任务</h3></div></div><div class="log-list"><div class="log-item" v-for="task in tasks.slice(0, 8)" :key="task.id"><div><strong>{{ task.channelType }} · 任务 #{{ task.id }}</strong><p>计划：{{ new Date(task.scheduledAt).toLocaleString() }}</p></div><div class="log-side"><span :class="['log-status', task.status === 'sent' ? 'success' : task.status === 'failed' ? 'failed' : task.status === 'processing' ? 'processing' : 'pending']">{{ task.status }}</span><small>{{ task.lastError || '等待调度或已成功投递' }}</small></div></div></div></section>
              <section class="subpanel log-scroll-panel"><div class="panel-head"><div><p class="section-label">Logs</p><h3>通知日志</h3></div></div><div class="log-list"><div class="log-item" v-for="log in logs.slice(0, 8)" :key="log.id"><div><strong>{{ log.channelName }}</strong><p>{{ log.message }}</p></div><div class="log-side"><span :class="['log-status', log.status]">{{ log.status }}</span><small>{{ new Date(log.triggeredAt).toLocaleString() }}</small></div></div></div></section>
            </div>
          </section>

          <section v-else-if="currentView === 'notify'" class="content-panel glass-panel">
            <div class="panel-head align-center">
              <div><p class="section-label">Notification</p><h2>通知相关</h2></div>
              <button class="primary-btn" @click="createGroup">新建通知组</button>
            </div>

            <section v-if="!notifyEditorVisible" class="subpanel">
              <div class="search-row">
                <input v-model="groupQuery" class="search-input" placeholder="检索通知组名称、成员名或目标地址" />
              </div>
              <div class="group-card-grid">
                <article class="memo-card" v-for="group in filteredNotifyGroups" :key="group.id">
                  <div class="group-card-head">
                    <div>
                      <h3>{{ group.name }}</h3>
                      <p>{{ group.enabled ? '已启用' : '未启用' }}</p>
                    </div>
                    <button class="secondary-btn" @click="editGroup(group)">进入配置</button>
                  </div>
                  <div class="tag-row"><span class="mini-tag" v-for="member in group.members" :key="member.id">{{ member.label }}（{{ member.type === 'email' ? '邮箱' : '钉钉' }}）</span></div>
                </article>
                <div v-if="filteredNotifyGroups.length === 0" class="lane-empty">没有匹配的通知组。</div>
              </div>
            </section>

            <section v-else class="subpanel">
              <div class="panel-head align-center">
                <div><p class="section-label">Notify Group Editor</p><h3>{{ groupForm.id ? `编辑通知组 #${groupForm.id}` : '新建通知组' }}</h3></div>
                <button class="secondary-btn" @click="closeGroupEditor">返回列表</button>
              </div>
              <p class="muted-copy" v-if="isGroupDirty">当前有未保存修改。</p>
              <div class="form-grid">
                <label class="field"><span>组名</span><input v-model="groupForm.name" placeholder="例如：工作提醒组" /></label>
                <label class="field"><span>启用状态</span><select v-model="groupForm.enabled"><option :value="true">启用</option><option :value="false">停用</option></select></label>
              </div>
              <div class="subsection">
                <span class="subsection-title">组成员</span>
                <div class="form-grid" v-for="(member, idx) in groupForm.members" :key="idx">
                  <label class="field"><span>类型</span><select v-model="member.type"><option value="email">邮箱</option><option value="dingtalk_webhook">钉钉 Webhook</option></select></label>
                  <label class="field"><span>名称</span><input v-model="member.label" placeholder="例如：值班邮箱 / 项目群机器人" /></label>
                  <label class="field field-full"><span>目标地址</span><input v-model="member.target" placeholder="邮箱地址 或 webhook 地址" /></label>
                  <label class="field" v-if="member.type === 'dingtalk_webhook'"><span>Secret（可选）</span><input v-model="member.secret" type="password" /></label>
                  <label class="field" v-if="member.type === 'dingtalk_webhook'"><span>关键词</span><input v-model="member.keyword" placeholder="例如：提醒" /></label>
                  <label class="field" v-if="member.type === 'dingtalk_webhook'"><span>启用加签</span><select v-model="member.useSign"><option :value="false">否</option><option :value="true">是</option></select></label>
                  <div class="field field-full">
                    <div class="member-test-row">
                    <button
                      class="secondary-btn"
                      :disabled="!member.target || testingMemberKey === `${member.type}-${idx}`"
                      @click="testGroupMember(member, idx)"
                    >
                      {{ testingMemberKey === `${member.type}-${idx}` ? '发送中...' : `发送${member.type === 'email' ? '邮箱' : '钉钉'}测试消息` }}
                    </button>
                    <span
                      v-if="memberTestStatus[`${member.type}-${idx}`] && memberTestStatus[`${member.type}-${idx}`].state !== 'idle'"
                      :class="[
                        'member-test-status',
                        memberTestStatus[`${member.type}-${idx}`].state === 'success' && 'success',
                        memberTestStatus[`${member.type}-${idx}`].state === 'error' && 'error',
                        memberTestStatus[`${member.type}-${idx}`].state === 'sending' && 'sending'
                      ]"
                    >
                      {{ memberTestStatus[`${member.type}-${idx}`].message }}
                    </span>
                    </div>
                  </div>
                </div>
                <div class="form-actions"><button class="secondary-btn" @click="addGroupMember">新增成员</button></div>
              </div>
              <div class="form-actions">
                <button class="primary-btn" :disabled="savingGroup" @click="submitNotifyGroup">{{ savingGroup ? '保存中...' : (groupForm.id ? '更新通知组' : '创建通知组') }}</button>
                <button class="secondary-btn" @click="closeGroupEditor">退出</button>
                <button v-if="groupForm.id" class="danger-btn" @click="removeGroup(groupForm.id)">删除通知组</button>
              </div>
            </section>
          </section>

          <section v-else-if="currentView === 'users'" class="content-panel glass-panel">
            <div class="panel-head align-center">
              <div><p class="section-label">Admin</p><h2>用户管理</h2></div>
            </div>
            <section class="subpanel">
              <div class="panel-head"><div><p class="section-label">Security</p><h3>登录限流设置</h3></div></div>
              <div class="form-grid">
                <label class="field">
                  <span>登录失败阈值</span>
                  <input v-model.number="loginPolicyForm.loginMaxFailed" type="number" min="1" max="20" />
                </label>
                <label class="field">
                  <span>限流窗口（秒）</span>
                  <input v-model.number="loginPolicyForm.loginWindowSec" type="number" min="30" max="3600" />
                </label>
              </div>
              <div class="form-actions">
                <button class="primary-btn" :disabled="savingLoginPolicy" @click="submitLoginPolicy">
                  {{ savingLoginPolicy ? '保存中...' : '保存登录限流设置' }}
                </button>
              </div>
            </section>
            <section class="subpanel top-gap">
              <section v-if="!userEditorVisible">
                <div class="panel-head align-center">
                  <div><p class="section-label">Users</p><h3>账号列表</h3></div>
                  <span class="lane-count">{{ managedUsers.length }}</span>
                </div>
                <div class="user-list-scroll" v-if="managedUsers.length > 0">
                  <div class="user-admin-grid">
                    <article class="user-admin-card clickable" v-for="user in pagedManagedUsers" :key="user.id" @click="openUserEditor(user)">
                      <div class="user-admin-head">
                        <div>
                          <strong>{{ user.name || user.username }}</strong>
                          <p>{{ user.username }} · {{ user.role === 'admin' ? '管理员' : '普通用户' }}</p>
                        </div>
                      </div>
                      <div class="mini-meta">点击进入编辑</div>
                    </article>
                  </div>
                </div>
                <div v-else class="lane-empty">暂无可管理用户。</div>
                <div class="pager-row" v-if="managedUsers.length > 0">
                  <div class="pager-left">
                    <span>每页显示</span>
                    <select v-model.number="userPageSize">
                      <option :value="12">12</option>
                      <option :value="30">30</option>
                      <option :value="50">50</option>
                      <option :value="100">100</option>
                    </select>
                    <span>条</span>
                  </div>
                  <div class="pager-right">
                    <button class="secondary-btn" :disabled="userPage <= 1" @click="userPage -= 1">上一页</button>
                    <span>{{ userPage }} / {{ userTotalPages }}</span>
                    <button class="secondary-btn" :disabled="userPage >= userTotalPages" @click="userPage += 1">下一页</button>
                  </div>
                </div>
              </section>
              <section v-else class="subpanel user-editor-panel">
                <div class="panel-head align-center">
                  <div><p class="section-label">User Editor</p><h3>{{ editingManagedUser?.username }}</h3></div>
                  <button class="secondary-btn" @click="closeUserEditor">返回列表</button>
                </div>
                <div v-if="editingManagedUser" class="form-grid">
                  <label class="field">
                    <span>新密码</span>
                    <input v-model="userPasswordDraft[editingManagedUser.id]" type="password" :placeholder="`为 ${editingManagedUser.username} 设置新密码`" />
                  </label>
                </div>
                <div v-if="editingManagedUser" class="form-actions">
                  <button class="secondary-btn" :disabled="resettingUserPasswordId > 0 || !userPasswordDraft[editingManagedUser.id]" @click="submitResetUserPassword(editingManagedUser)">
                    {{ resettingUserPasswordId === editingManagedUser.id ? '更新中...' : '修改密码' }}
                  </button>
                  <button class="danger-btn" :disabled="deletingUser || editingManagedUser.id === currentUser?.id || editingManagedUser.role === 'admin'" @click="removeUser(editingManagedUser)">
                    {{ deletingUser ? '删除中...' : editingManagedUser.id === currentUser?.id ? '当前账号' : editingManagedUser.role === 'admin' ? '管理员不可删' : '删除用户' }}
                  </button>
                </div>
              </section>
            </section>
            <section class="subpanel top-gap">
              <div class="panel-head align-center">
                <div><p class="section-label">Audit</p><h3>管理员操作日志</h3></div>
                <span class="lane-count">{{ auditTotal }}</span>
              </div>
              <div class="log-list audit-list" v-if="adminAuditLogs.length > 0">
                <div class="log-item" v-for="item in adminAuditLogs" :key="item.id">
                  <div>
                    <strong>{{ item.actorUsername }} · {{ item.action }}</strong>
                    <p>{{ item.detail }}（目标：{{ item.targetUsername || '-' }}）</p>
                  </div>
                  <div class="log-side">
                    <span class="log-status pending">audit</span>
                    <small>{{ new Date(item.createdAt).toLocaleString() }}</small>
                  </div>
                </div>
              </div>
              <div v-else class="lane-empty">{{ loadingAudit ? '加载中...' : '暂无审计日志。' }}</div>
              <div class="pager-row" v-if="auditTotal > 0">
                <div class="pager-left">
                  <span>每页显示</span>
                  <select v-model.number="auditPageSize">
                    <option :value="12">12</option>
                    <option :value="30">30</option>
                    <option :value="50">50</option>
                    <option :value="100">100</option>
                  </select>
                  <span>条</span>
                </div>
                <div class="pager-right">
                  <button class="secondary-btn" :disabled="auditPage <= 1 || loadingAudit" @click="auditPage -= 1">上一页</button>
                  <span>{{ auditPage }} / {{ auditTotalPages }}</span>
                  <button class="secondary-btn" :disabled="auditPage >= auditTotalPages || loadingAudit" @click="auditPage += 1">下一页</button>
                </div>
              </div>
            </section>
          </section>

          <section v-else class="content-panel glass-panel">
            <div class="panel-head"><div><p class="section-label">Workspace Settings</p><h2>系统设置与显示配置</h2></div></div>
            <div class="settings-stack">
              <section class="channel-overview">
                <article class="channel-status-card">
                  <div class="channel-status-head">
                    <div>
                      <p class="section-label">Mail Channel</p>
                      <h3>邮箱提醒</h3>
                    </div>
                    <span :class="['status-chip', emailReady ? 'ready' : 'missing']">{{ emailReady ? '已就绪' : '未配置完成' }}</span>
                  </div>
                  <p>{{ emailReady ? `当前发件邮箱：${mailForm.fromAddress}` : '请补充 SMTP 主机、账号、发件邮箱后即可发送邮件提醒。' }}</p>
                  <div class="channel-meta-row">
                    <span>启用状态：{{ mailForm.enabled ? '已启用' : '未启用' }}</span>
                    <span>测试邮箱：{{ reminderDraft.testMailTo || '未填写' }}</span>
                  </div>
                </article>
              </section>

              <section class="subpanel">
                <div class="panel-head"><div><p class="section-label">Brand</p><h3>界面信息</h3></div></div>
                <div class="form-grid">
                  <label class="field"><span>项目名称</span><input v-model="uiSettings.projectName" /></label>
                  <label class="field"><span>项目副标题</span><input v-model="uiSettings.slogan" /></label>
                  <label class="field"><span>显示用户名</span><input v-model="uiSettings.displayName" /></label>
                  <label class="field"><span>个人简介</span><input v-model="uiSettings.displaySubTitle" /></label>
                </div>
                <div class="form-actions"><button class="primary-btn" :disabled="savingUi" @click="saveUiSettings">{{ savingUi ? '保存中...' : '保存界面设置' }}</button></div>
              </section>

              <section class="subpanel">
                <div class="panel-head"><div><p class="section-label">SMTP</p><h3>邮件发送配置</h3></div></div>
                <button class="collapse-btn" @click="showMailConfig = !showMailConfig">{{ showMailConfig ? '收起' : '展开' }}</button>
                <div v-if="showMailConfig">
                <div class="pill-switch"><button :class="['pill-btn', mailForm.enabled && 'active']" @click="mailForm.enabled = true">启用</button><button :class="['pill-btn', !mailForm.enabled && 'active']" @click="mailForm.enabled = false">停用</button></div>
                <div class="form-grid">
                  <label class="field"><span>SMTP 主机</span><input v-model="mailForm.host" /></label>
                  <label class="field"><span>端口</span><input v-model.number="mailForm.port" type="number" /></label>
                  <label class="field"><span>账号</span><input v-model="mailForm.username" /></label>
                  <label class="field">
                    <span>密码 / 授权码</span>
                    <input v-model="mailForm.password" type="password" :placeholder="mailPasswordSaved ? '已保存，留空表示保持不变' : ''" />
                    <small v-if="mailPasswordSaved" class="field-hint">当前已保存密码，如需更换再输入新值。</small>
                  </label>
                  <label class="field"><span>发件人名称</span><input v-model="mailForm.fromName" /></label>
                  <label class="field"><span>发件人邮箱</span><input v-model="mailForm.fromAddress" /></label>
                  <label class="field field-full"><span>测试收件邮箱</span><input v-model="reminderDraft.testMailTo" /></label>
                </div>
                <div class="toggle-row">
                  <label class="checkbox-line"><input v-model="mailForm.useTls" type="checkbox" /><span>使用 TLS</span></label>
                  <label class="checkbox-line"><input v-model="mailForm.useSsl" type="checkbox" /><span>使用 SSL</span></label>
                </div>
                <p class="field-hint">建议：端口 465 使用 SSL；端口 587 使用 TLS（STARTTLS）。</p>
                <div class="form-actions">
                  <button class="primary-btn" :disabled="savingMail" @click="submitMailConfig">{{ savingMail ? '保存中...' : '保存 SMTP 配置' }}</button>
                  <button class="secondary-btn" :disabled="testingMail" @click="testMailConfig">{{ testingMail ? '发送中...' : '发送测试邮件' }}</button>
                  <button class="secondary-btn" :disabled="diagnosingMail" @click="runMailDiagnosis">{{ diagnosingMail ? '诊断中...' : '诊断 SMTP 连接' }}</button>
                </div>
                <div v-if="mailDiagnosis" class="diagnose-panel">
                  <p class="diagnose-title">诊断主机：{{ mailDiagnosis.host }}</p>
                  <div class="diagnose-list">
                    <div v-for="(item, index) in mailDiagnosis.steps" :key="`${item.port}-${item.step}-${index}`" class="diagnose-item">
                      <span :class="['diagnose-status', item.ok ? 'ok' : 'bad']">{{ item.ok ? '通过' : '失败' }}</span>
                      <span class="diagnose-step">端口 {{ item.port }} · {{ item.step }}</span>
                      <span class="diagnose-latency">{{ item.latencyMs }}ms</span>
                      <small v-if="item.error" class="diagnose-error">{{ item.error }}</small>
                    </div>
                  </div>
                </div>
                </div>
              </section>
            </div>
          </section>
        </main>
      </section>
    </div>

    <div v-if="confirmState.visible" class="confirm-mask" @click="closeConfirm(false)">
      <section class="confirm-dialog glass-panel" @click.stop>
        <p class="section-label">Confirm</p>
        <h3>{{ confirmState.title }}</h3>
        <p class="muted-copy">{{ confirmState.message }}</p>
        <div class="form-actions top-gap">
          <button class="secondary-btn" @click="closeConfirm(false)">{{ confirmState.cancelText }}</button>
          <button class="primary-btn" @click="closeConfirm(true)">{{ confirmState.confirmText }}</button>
        </div>
      </section>
    </div>
  </div>
</template>
