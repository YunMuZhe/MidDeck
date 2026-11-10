export type Kind = 'kafka' | 'redis' | 'rabbitmq'

export interface SSHHop {
  bastionId?: string
  host: string
  port: number
  user: string
  authType: 'password' | 'key' | 'agent'
  keyPath?: string
  secret?: string
}

export interface Bastion extends SSHHop { id: string; name: string; description?: string }
export interface BastionView extends Bastion { hasSecret: boolean; usedBy: string[] | null }

export interface Instance {
  id: string
  name: string
  kind: Kind
  project: string
  env: string
  tags: string[]
  aliases: string[]
  description: string
  readOnly: boolean
  addrs: string[]
  redisMode?: string
  redisDb?: number
  masterSet?: string
  mgmtUrl?: string
  vhost?: string
  auth: { mechanism?: string; username?: string; password?: string }
  tls: { enabled: boolean; insecureSkipVerify: boolean; caFile?: string; certFile?: string; keyFile?: string }
  network: {
    proxy: { type: string; addr: string; user?: string; secret?: string }
    ssh: SSHHop[]
    addrMap?: Record<string, string>
  }
  registry?: { url?: string; username?: string; password?: string; insecureSkipVerify?: boolean }
  proto?: { paths?: string[]; bindings?: ProtoBinding[] }
}

export interface Step {
  name: string
  status: 'ok' | 'warn' | 'fail' | 'skip'
  detail?: string
  hint?: string
  durationMs: number
}

export interface Report {
  steps: Step[]
  ok: boolean
}

export interface CompatResult {
  product: string
  version: string
  level: 'tested' | 'supported' | 'partial' | 'unsupported' | 'unknown'
  message: string
  unavailable: string[] | null
}

export interface ServerInfo {
  kind: string
  version: string
  mode: string
  extra?: Record<string, string>
  compat: CompatResult
}

export interface ConnectResult {
  info: ServerInfo
  report?: Report
}

// Kafka
export interface Topic { name: string; partitions: number; replication: number; internal: boolean; error?: string }
export interface Partition { id: number; leader: number; replicas: number[]; isr: number[]; startOffset: number; endOffset: number }
export interface ConfigEntry { key: string; value: string; source: string; sensitive: boolean }
export interface TopicDetail extends Topic { partitionList: Partition[]; configs: ConfigEntry[] | null; messages: number }
export interface Header { key: string; value: string }
export interface Message {
  partition: number; offset: number; timestamp: number; key: string; value: string; headers: Header[] | null; size: number
  keyFormat?: string; valueFormat?: string; keySchema?: string; valueSchema?: string; decodeError?: string
}
export interface FetchReq {
  topic: string; partition: number; mode: string; offset: number; timestamp: number; limit: number; keyFilter: string; contains: string
  decode?: '' | 'raw'; protoType?: string
}
export interface ProtoBinding { topic: string; key?: string; value?: string }
export interface ProtoTypes { types: string[]; problems: string[]; bindings: ProtoBinding[]; paths: string[] | null; registry: string }
export interface SchemaSubject { name: string; versions: number[] | null; latest: number; type: string; id: number }
export interface RegistrySchema { id: number; subject?: string; version?: number; schemaType: string; schema: string; references?: { name: string; subject: string; version: number }[] | null }
export interface GroupSummary { group: string; state: string; members: number; lag: number; topics: string[] | null; error?: string }
export interface PartitionLag { topic: string; partition: number; committed: number; end: number; lag: number; member: string; host: string }
export interface GroupDetail extends GroupSummary { partitions: PartitionLag[] | null }
export interface OffsetChange { partition: number; from: number; to: number }

// Redis
export interface KeyItem { key: string; type: string; ttl: number }
export interface ScanResult { keys: KeyItem[] | null; cursor: number; done: boolean }
export interface Field { field?: string; value: string; score?: number; id?: string }
export interface KeyValue { key: string; type: string; ttl: number; size: number; memory: number; string?: string; items: Field[] | null; truncated: boolean }
export interface SlowLog { id: number; time: number; durationUs: number; command: string; client: string }

// RabbitMQ
export interface Queue {
  name: string; vhost: string; type: string; durable: boolean; auto_delete: boolean; state: string
  messages: number; messages_ready: number; messages_unacknowledged: number; consumers: number
  arguments: Record<string, unknown> | null; node: string
}
export interface Exchange { name: string; vhost: string; type: string; durable: boolean; auto_delete: boolean; internal: boolean }
export interface Binding { source: string; destination: string; destination_type: string; routing_key: string }
export interface PeekedMessage {
  payload: string; payload_encoding: string; payload_bytes: number; redelivered: boolean
  exchange: string; routing_key: string; message_count: number; properties: Record<string, unknown> | null
}

export interface AuditEntry { time: string; instance: string; name: string; env: string; action: string; detail: unknown; error?: string }
export interface ImportResult { added: number; overwritten: number; skipped: number }
export interface Feature { key: string; label: string; min: string }
export interface Matrix { product: string; tested: string[]; supportedMin: string; partialMin: string; features: Feature[] }

export const emptyInstance = (kind: Kind = 'kafka'): Instance => ({
  id: '', name: '', kind, project: '', env: 'dev', tags: [], aliases: [], description: '', readOnly: false,
  addrs: [], redisMode: 'standalone', redisDb: 0, masterSet: '', mgmtUrl: '', vhost: '/',
  auth: { mechanism: 'none', username: '', password: '' },
  tls: { enabled: false, insecureSkipVerify: false },
  network: { proxy: { type: '', addr: '' }, ssh: [], addrMap: {} },
  registry: { url: '', username: '', password: '', insecureSkipVerify: false },
  proto: { paths: [], bindings: [] },
})

// AI assistant
export interface AISettings {
  enabled: boolean
  provider: 'anthropic' | 'openai'
  baseUrl: string
  model: string
  apiKey?: string
  proxy?: string
  sendAddresses: boolean
  hasKey: boolean
}
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type ActionArgs = Record<string, any>
export interface AIAction { action: string; args: ActionArgs | null; text?: string }
/** A proposed action handed to a view to prefill; seq changes for every new proposal. */
export interface Intent { seq: number; action: string; args: ActionArgs }

// Changesets
export interface Diff { field: string; from: string; to: string }
export interface Change { resource: string; name: string; action: 'create' | 'update' | 'delete' | 'conflict'; diffs: Diff[] | null; notes: string[] | null }
export interface Plan { kind: string; env: string; changes: Change[]; unchanged: number; warnings: string[]; fingerprint: string }
export interface ChangeResult extends Change { status: 'applied' | 'failed' | 'skipped'; error?: string }
export interface TextFile { path: string; name: string; content: string }

// Skip & replay
export interface OffsetRange { partition: number; from: number; to: number }
export interface SkipPreview { ranges: OffsetRange[]; messages: Message[]; total: number; state: string }
export interface Replay { at: string; instanceId: string; instanceName: string; topic: string; keepPartitions: boolean; count: number; error?: string }
export interface ParkedBatch {
  id: string; instanceId: string; instanceName: string; group: string; topic: string; reason?: string; createdAt: string
  count: number; bytes: number; ranges: OffsetRange[]; skipped: boolean; replays: Replay[]; dir: string
}

// Team repository
export interface Person { name: string; email: string }
export interface TeamStatus {
  configured: boolean; repo: string; branch: string; upstream: string; ahead: number; behind: number; dirty: number
  user: Person; error?: string
}
export interface CatalogEntry { id: string; name: string; kind: Kind; project: string; env: string; status: 'same' | 'changed' | 'teamOnly' | 'localOnly' }
export interface LibraryFile { name: string; modified: string; size: number }

// Patrol alerts
export type WatchType = 'kafka_lag' | 'rabbit_dlq' | 'redis_bigkey'
export interface WatchRule {
  id: string; name: string; instanceId: string; type: WatchType; target: string
  threshold: number; growth: number; intervalSec: number; enabled: boolean
}
export interface WatchItem { name: string; value: number; delta?: number }
export interface WatchStatus { ruleId: string; state: 'idle' | 'ok' | 'firing' | 'error'; checkedAt: string; since: string; worst: WatchItem[] | null; checked: number; error?: string }
export interface WatchAlert { time: string; ruleId: string; rule: string; type: WatchType; instance: string; kind: 'firing' | 'reminder' | 'recovered' | 'error'; items?: WatchItem[]; error?: string }
export interface WatchState { rules: WatchRule[] | null; statuses: WatchStatus[] | null; alerts: WatchAlert[] | null }
