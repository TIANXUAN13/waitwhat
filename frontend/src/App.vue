<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
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
import type { AppState, AuthUser, DatabaseDriver, NotificationGroup } from './types'

interface ReminderOption {
  key: string
  label: string
  offsetMin: number
  selected: boolean
}

type ViewMode = 'composer' | 'list' | 'notify' | 'settings'
type AuthMode = 'login' | 'register'

const SETTINGS_KEY = 'waitwhat-ui-settings'
const LOGIN_READY_HINT_SEEN_KEY = 'waitwhat-login-ready-hint-seen'

const loading = ref(true)
const submitting = ref(false)
const creating = ref(false)
const savingMail = ref(false)
const testingMail = ref(false)
const diagnosingMail = ref(false)
const dispatching = ref(false)
const savingUi = ref(false)
const authSubmitting = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const appState = ref<AppState | null>(null)
const currentView = ref<ViewMode>('composer')
const listFocus = ref<'pending' | 'expired'>('pending')
const authMode = ref<AuthMode>('login')
const editingEventId = ref<number | null>(null)
const showLoginReadyHint = ref(false)
const showMailConfig = ref(true)
const showResetInit = computed(() => /no such column|SQL logic error|database/i.test(errorMessage.value))

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

const uiSettings = reactive({
  projectName: 'WaitWhat',
  slogan: '清新的提醒工作台',
  displayName: 'Chen',
  displaySubTitle: '专注你的每个关键节点'
})

const reminderDraft = reactive({
  title: '新的事项提醒',
  content: '这里可以写会议准备、账单提醒、待办安排等。',
  eventAt: '2026-03-20T18:30',
  customValue: 0,
  customUnit: 'minute',
  testMailTo: ''
})

const reminderOptions = reactive<ReminderOption[]>([
  { key: 'day-1', label: '提前 1 天', offsetMin: 1440, selected: true },
  { key: 'hour-2', label: '提前 2 小时', offsetMin: 120, selected: true },
  { key: 'min-10', label: '提前 10 分钟', offsetMin: 10, selected: true }
])

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
const notifyGroups = computed<NotificationGroup[]>(() => appState.value?.notifyGroups ?? [])
const events = computed(() => appState.value?.events ?? [])
const tasks = computed(() => appState.value?.tasks ?? [])
const logs = computed(() => appState.value?.logs ?? [])
const pendingEvents = computed(() =>
  events.value
    .filter((event) => new Date(event.eventAt).getTime() >= Date.now())
    .sort((a, b) => new Date(a.eventAt).getTime() - new Date(b.eventAt).getTime())
)
const expiredEvents = computed(() =>
  events.value
    .filter((event) => new Date(event.eventAt).getTime() < Date.now())
    .sort((a, b) => new Date(b.eventAt).getTime() - new Date(a.eventAt).getTime())
)
const filteredNotifyGroups = computed(() => {
  const keyword = groupQuery.value.trim().toLowerCase()
  if (!keyword) return notifyGroups.value
  return notifyGroups.value.filter((group) => {
    if (group.name.toLowerCase().includes(keyword)) return true
    return group.members.some((member) => member.label.toLowerCase().includes(keyword) || member.target.toLowerCase().includes(keyword))
  })
})
const groupDraftSnapshot = ref('')
const isGroupDirty = computed(() => groupDraftSnapshot.value !== serializeGroupDraft())

const selectedReminderSummary = computed(() => {
  const items = reminderOptions.filter((item) => item.selected).map((item) => item.label)
  const custom = customReminderLabel()
  if (custom) items.push(custom)
  items.push('到点提醒')
  return items
})

const selectedGroups = computed(() => notifyGroups.value.filter((group) => selectedGroupIds.value.includes(group.id)))

const summaryStats = computed(() => ({
  total: events.value.length,
  pending: tasks.value.filter((task) => task.status === 'pending').length,
  success: logs.value.filter((log) => log.status === 'success').length
}))

const emailReady = computed(() => Boolean(mailForm.enabled && mailForm.host && mailForm.fromAddress))

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

function loadUiSettings() {
  const raw = localStorage.getItem(SETTINGS_KEY)
  if (!raw) return
  try {
    Object.assign(uiSettings, JSON.parse(raw))
  } catch {
    return
  }
}

function saveUiSettings() {
  savingUi.value = true
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(uiSettings))
  successMessage.value = '界面设置已保存。'
  setTimeout(() => {
    savingUi.value = false
  }, 250)
}

function customReminderLabel() {
  if (!reminderDraft.customValue || reminderDraft.customValue <= 0) return ''
  const unitMap: Record<string, string> = { minute: '分钟', hour: '小时', day: '天' }
  return `提前 ${reminderDraft.customValue} ${unitMap[reminderDraft.customUnit]}`
}

function customReminderOffsetMin() {
  const value = Number(reminderDraft.customValue)
  if (!value || value <= 0) return 0
  if (reminderDraft.customUnit === 'day') return value * 1440
  if (reminderDraft.customUnit === 'hour') return value * 60
  return value
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

async function loadData() {
  loading.value = true
  resetMessages()
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
      selectedGroupIds.value = (appState.value.notifyGroups ?? []).filter((group) => group.enabled).map((group) => group.id)
      if (currentUser.value) {
        uiSettings.displayName = currentUser.value.name || currentUser.value.username
      }
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载失败'
  } finally {
    loading.value = false
  }
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
    const customOffset = customReminderOffsetMin()
    const customLabel = customReminderLabel()
    if (customOffset > 0 && customLabel) {
      reminderPoints.push({ id: Date.now() + 99, label: customLabel, offsetMin: customOffset })
    }
    if (reminderPoints.length === 0) throw new Error('请至少选择一个预提醒时间')
    if (selectedGroupIds.value.length === 0) throw new Error('请至少选择一个通知组')

    const payload = {
      userId: currentUser.value?.id ?? 0,
      title: reminderDraft.title,
      content: reminderDraft.content,
      eventAt: toApiDateTime(reminderDraft.eventAt),
      reminderEnabled: true,
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
    }
    editingEventId.value = null
    switchView('list')
    await loadData()
  } catch (error) {
    errorMessage.value = normalizeErrorMessage(error, '事件创建失败')
  } finally {
    creating.value = false
  }
}

function beginEdit(event: AppState['events'][number]) {
  switchView('composer')
  editingEventId.value = event.id
  reminderDraft.title = event.title
  reminderDraft.content = event.content
  reminderDraft.eventAt = toDateTimeLocal(event.eventAt)
  selectedGroupIds.value = [...(event.boundGroupIds ?? [])]
  reminderOptions.forEach((option) => {
    option.selected = event.reminderPoints.some((point) => point.offsetMin === option.offsetMin)
  })
  const customPoint = event.reminderPoints.find((point) => !reminderOptions.some((option) => option.offsetMin === point.offsetMin))
  if (customPoint) {
    if (customPoint.offsetMin % 1440 === 0) {
      reminderDraft.customUnit = 'day'
      reminderDraft.customValue = customPoint.offsetMin / 1440
    } else if (customPoint.offsetMin % 60 === 0) {
      reminderDraft.customUnit = 'hour'
      reminderDraft.customValue = customPoint.offsetMin / 60
    } else {
      reminderDraft.customUnit = 'minute'
      reminderDraft.customValue = customPoint.offsetMin
    }
  } else {
    reminderDraft.customUnit = 'minute'
    reminderDraft.customValue = 0
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
    successMessage.value = `扫描完成：触发 ${response.result.triggered}，成功 ${response.result.sent}，失败 ${response.result.failed}，跳过 ${response.result.skipped}。`
    await loadData()
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
    await loadData()
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
    await loadData()
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
      <div class="auth-card">
        <p class="eyebrow">Welcome</p>
        <h1>{{ uiSettings.projectName }}</h1>
        <p v-if="showLoginReadyHint" class="muted-copy">管理员已设置完成。现在可以登录，或者注册普通用户再进入工作台。</p>
        <div class="pill-switch">
          <button :class="['pill-btn', authMode === 'login' && 'active']" @click="authMode = 'login'">登录</button>
          <button :class="['pill-btn', authMode === 'register' && 'active']" @click="authMode = 'register'">注册</button>
        </div>
        <div class="form-grid">
          <label class="field"><span>用户名</span><input v-model="authForm.username" /></label>
          <label class="field"><span>密码</span><input v-model="authForm.password" type="password" /></label>
          <label v-if="authMode === 'register'" class="field"><span>昵称</span><input v-model="authForm.name" /></label>
          <label v-if="authMode === 'register'" class="field"><span>邮箱</span><input v-model="authForm.email" /></label>
        </div>
        <div class="form-actions">
          <button class="primary-btn" :disabled="authSubmitting" @click="submitAuth">{{ authSubmitting ? '提交中...' : authMode === 'login' ? '登录' : '注册并登录' }}</button>
        </div>
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
                <label class="field"><span>事件时间</span><input v-model="reminderDraft.eventAt" type="datetime-local" /></label>
                <div class="subsection">
                  <span class="subsection-title">预提醒时间</span>
                  <div class="selectable-grid">
                    <button v-for="option in reminderOptions" :key="option.key" type="button" :class="['select-chip', option.selected && 'active']" @click="option.selected = !option.selected">{{ option.label }}</button>
                  </div>
                  <div class="custom-reminder-row">
                    <label class="field compact-field"><span>自定义提前</span><input v-model.number="reminderDraft.customValue" type="number" min="1" /></label>
                    <label class="field compact-field"><span>单位</span><select v-model="reminderDraft.customUnit"><option value="minute">分钟</option><option value="hour">小时</option><option value="day">天</option></select></label>
                  </div>
                </div>
                <div class="subsection">
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
                <div class="form-actions">
                  <button class="primary-btn" :disabled="creating" @click="submitEvent">{{ creating ? '处理中...' : editingEventId ? '保存修改' : '创建这个事件' }}</button>
                  <button v-if="editingEventId" class="secondary-btn" @click="editingEventId = null">取消编辑</button>
                </div>
              </div>

              <div class="preview-board">
                <div class="preview-summary"><p class="section-label">Preview</p><h3>{{ reminderDraft.title }}</h3><p>{{ reminderDraft.content }}</p></div>
                <div class="info-card"><span>事件时间</span><strong>{{ reminderDraft.eventAt || '请选择时间' }}</strong></div>
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
            <div class="memo-lanes top-gap">
              <section v-if="listFocus === 'pending'" :class="['subpanel', 'memo-lane', 'active-lane']">
                <div class="lane-head">
                  <h3>待提醒</h3>
                  <span class="lane-count">{{ pendingEvents.length }}</span>
                </div>
                <div v-if="pendingEvents.length === 0" class="lane-empty">暂无待提醒备忘录。</div>
                <div v-else class="list-stack">
                  <article class="memo-card" v-for="event in pendingEvents" :key="event.id">
                    <div class="memo-header"><div><h3>{{ event.title }}</h3><p>{{ event.content }}</p></div><span class="countdown-badge">{{ event.countdownLabel }}</span></div>
                    <div class="meta-row"><span>事件时间：{{ new Date(event.eventAt).toLocaleString() }}</span><span>状态：{{ event.status }}</span></div>
                    <div class="tag-row"><span class="mini-tag" v-for="point in event.reminderPoints" :key="point.id">{{ point.label }}</span></div>
                    <div class="form-actions top-gap">
                      <button class="secondary-btn" @click="beginEdit(event)">编辑</button>
                      <button class="danger-btn" @click="removeEvent(event.id)">删除</button>
                    </div>
                  </article>
                </div>
              </section>
              <section v-else :class="['subpanel', 'memo-lane', 'active-lane']">
                <div class="lane-head">
                  <h3>已过期</h3>
                  <span class="lane-count">{{ expiredEvents.length }}</span>
                </div>
                <div v-if="expiredEvents.length === 0" class="lane-empty">暂无过期备忘录。</div>
                <div v-else class="list-stack">
                  <article class="memo-card" v-for="event in expiredEvents" :key="event.id">
                    <div class="memo-header"><div><h3>{{ event.title }}</h3><p>{{ event.content }}</p></div><span class="countdown-badge">{{ event.countdownLabel }}</span></div>
                    <div class="meta-row"><span>事件时间：{{ new Date(event.eventAt).toLocaleString() }}</span><span>状态：{{ event.status }}</span></div>
                    <div class="tag-row"><span class="mini-tag" v-for="point in event.reminderPoints" :key="point.id">{{ point.label }}</span></div>
                    <div class="form-actions top-gap">
                      <button class="secondary-btn" @click="beginEdit(event)">编辑</button>
                      <button class="danger-btn" @click="removeEvent(event.id)">删除</button>
                    </div>
                  </article>
                </div>
              </section>
            </div>
            <div class="dual-grid">
              <section class="subpanel"><div class="panel-head"><div><p class="section-label">Tasks</p><h3>提醒任务</h3></div></div><div class="log-list"><div class="log-item" v-for="task in tasks.slice(0, 8)" :key="task.id"><div><strong>{{ task.channelType }} · 任务 #{{ task.id }}</strong><p>计划：{{ new Date(task.scheduledAt).toLocaleString() }}</p></div><div class="log-side"><span :class="['log-status', task.status === 'sent' ? 'success' : task.status === 'failed' ? 'failed' : 'pending']">{{ task.status }}</span><small>{{ task.lastError || '等待调度或已成功投递' }}</small></div></div></div></section>
              <section class="subpanel"><div class="panel-head"><div><p class="section-label">Logs</p><h3>通知日志</h3></div></div><div class="log-list"><div class="log-item" v-for="log in logs.slice(0, 8)" :key="log.id"><div><strong>{{ log.channelName }}</strong><p>{{ log.message }}</p></div><div class="log-side"><span :class="['log-status', log.status]">{{ log.status }}</span><small>{{ new Date(log.triggeredAt).toLocaleString() }}</small></div></div></div></section>
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
