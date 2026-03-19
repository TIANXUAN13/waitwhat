export type DatabaseDriver = 'sqlite' | 'postgres'

export interface DatabaseConfig {
  selectedDriver: DatabaseDriver
  sqlitePath: string
  pgHost: string
  pgPort: number
  pgDatabase: string
  pgUser: string
  pgSslMode: string
  initializedAt: string
}

export interface MailConfig {
  enabled: boolean
  host: string
  port: number
  username: string
  hasPassword: boolean
  fromName: string
  fromAddress: string
  useTls: boolean
  useSsl: boolean
  initialized: string
}

export interface DingTalkConfig {
  enabled: boolean
  webhook: string
  useSign: boolean
  keyword: string
  initialized: string
}

export interface NotificationChannel {
  id: number
  userId: number
  type: string
  name: string
  target: string
  enabled: boolean
  lastChecked: string
}

export interface User {
  id: number
  username: string
  name: string
  email: string
  role: string
  channels: NotificationChannel[]
}

export interface ReminderPoint {
  id: number
  label: string
  offsetMin: number
}

export interface NotificationGroupMember {
  id: number
  groupId: number
  type: 'email' | 'dingtalk_webhook'
  label: string
  target: string
  keyword: string
  useSign: boolean
  enabled: boolean
}

export interface NotificationGroup {
  id: number
  userId: number
  name: string
  enabled: boolean
  members: NotificationGroupMember[]
}

export interface UpcomingNotifyPoint {
  label: string
  notifyAt: string
  channelSummary: string
}

export interface MemoEvent {
  id: number
  userId: number
  title: string
  content: string
  eventAt: string
  reminderEnabled: boolean
  reminderPoints: ReminderPoint[]
  boundChannelIds: number[]
  boundGroupIds: number[]
  countdownLabel: string
  status: string
  createdAt: string
  updatedAt: string
  upcomingNotifyPlan: UpcomingNotifyPoint[]
}

export interface NotifyLog {
  id: number
  eventId: number
  reminderId: number
  channelType: string
  channelName: string
  status: string
  message: string
  triggeredAt: string
}

export interface ReminderTask {
  id: number
  eventId: number
  reminderId: number
  channelId: number
  channelType: string
  status: string
  scheduledAt: string
  triggeredAt: string
  lastError: string
}

export interface AuthUser {
  id: number
  username: string
  name: string
  email: string
  role: string
  channels: NotificationChannel[]
}

export interface AuthState {
  adminExists: boolean
  currentUser: AuthUser | null
}

export interface AppState {
  database: DatabaseConfig
  mail: MailConfig
  dingTalk: DingTalkConfig
  auth: AuthState
  users: User[]
  events: MemoEvent[]
  tasks: ReminderTask[]
  logs: NotifyLog[]
  notifyGroups: NotificationGroup[]
}
