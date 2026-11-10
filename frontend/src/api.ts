// Thin wrapper over the Wails bindings (window.go.main.App.*), typed by hand so the
// frontend type-checks without the generated wailsjs directory.
import type {
  AIAction, AISettings, AuditEntry, Bastion, BastionView, Binding, ConnectResult, Exchange, FetchReq, GroupDetail, GroupSummary, ImportResult, Instance,
  KeyValue, Matrix, Message, OffsetChange, PeekedMessage, Queue, Report, ScanResult, SlowLog, Topic, TopicDetail,
  ChangeResult, ParkedBatch, Plan, Replay, SkipPreview, TextFile, ProtoBinding, ProtoTypes, SchemaSubject, RegistrySchema,
  CatalogEntry, LibraryFile, TeamStatus, WatchRule, WatchState, WatchStatus,
} from './types'

export interface SkipReq { group: string; topic: string; counts: Record<number, number> }

declare global {
  interface Window {
    go?: { main?: { App?: Record<string, (...args: unknown[]) => Promise<unknown>> } }
  }
}

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const fn = window.go?.main?.App?.[method]
  if (!fn) return Promise.reject(new Error(`后端方法 ${method} 不可用（请在 MidDeck 应用内运行）`))
  return fn(...args) as Promise<T>
}

/** Subscribe to a backend (Wails) event; returns the unsubscribe function. */
export function onBackendEvent<T>(name: string, fn: (data: T) => void): () => void {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const rt = (window as any).runtime
  if (!rt?.EventsOn) return () => {}
  const off = rt.EventsOn(name, fn)
  return typeof off === 'function' ? off : () => rt.EventsOff?.(name)
}

export const api = {
  version: () => call<string>('Version'),
  setLanguage: (l: string) => call<void>('SetLanguage', l),
  listInstances: () => call<Instance[] | null>('ListInstances').then((x) => x ?? []),
  getInstance: (id: string) => call<Instance>('GetInstance', id),
  saveInstance: (i: Instance) => call<Instance>('SaveInstance', i),
  deleteInstance: (id: string) => call<void>('DeleteInstance', id),
  exportInstances: (ids: string[], passphrase: string) => call<string>('ExportInstances', ids, passphrase),
  importInstances: (passphrase: string, policy: string) => call<ImportResult>('ImportInstances', passphrase, policy),
  listBastions: () => call<BastionView[] | null>('ListBastions').then((x) => x ?? []),
  saveBastion: (b: Bastion) => call<Bastion>('SaveBastion', b),
  deleteBastion: (id: string) => call<void>('DeleteBastion', id),
  testBastion: (b: Bastion) => call<Report>('TestBastion', b),
  mcpInfo: () => call<{ command: string; args: string[] }>('MCPInfo'),
  getAISettings: () => call<AISettings>('GetAISettings'),
  saveAISettings: (s: AISettings) => call<void>('SaveAISettings', s),
  testAI: (s: AISettings) => call<string>('TestAI', s),
  askAI: (prompt: string, current: string) => call<AIAction>('AskAI', prompt, current),
  testConnection: (i: Instance) => call<Report>('TestConnection', i),
  connect: (id: string) => call<ConnectResult>('Connect', id),
  disconnect: (id: string) => call<void>('Disconnect', id),
  auditLog: (n: number) => call<AuditEntry[] | null>('AuditLog', n).then((x) => x ?? []),
  compatMatrix: () => call<Record<string, Matrix>>('CompatMatrix'),

  kafkaTopics: (id: string) => call<Topic[] | null>('KafkaTopics', id).then((x) => x ?? []),
  kafkaDescribeTopic: (id: string, t: string) => call<TopicDetail>('KafkaDescribeTopic', id, t),
  kafkaCreateTopic: (id: string, r: { name: string; partitions: number; replication: number; configs: Record<string, string> }) =>
    call<void>('KafkaCreateTopic', id, r),
  kafkaDeleteTopic: (id: string, t: string) => call<void>('KafkaDeleteTopic', id, t),
  kafkaSetPartitions: (id: string, t: string, n: number) => call<void>('KafkaSetPartitions', id, t, n),
  kafkaAlterTopicConfigs: (id: string, t: string, set: Record<string, string>) => call<void>('KafkaAlterTopicConfigs', id, t, set),
  kafkaSchemaSubjects: (id: string) => call<SchemaSubject[] | null>('KafkaSchemaSubjects', id).then((x) => x ?? []),
  kafkaSchema: (id: string, subject: string, version: string) => call<RegistrySchema>('KafkaSchema', id, subject, version),
  kafkaProtoTypes: (id: string, reload: boolean) => call<ProtoTypes>('KafkaProtoTypes', id, reload),
  kafkaSetProtoBinding: (id: string, b: ProtoBinding) => call<void>('KafkaSetProtoBinding', id, b),
  kafkaFetch: (id: string, r: FetchReq) => call<Message[] | null>('KafkaFetch', id, r).then((x) => x ?? []),
  kafkaProduce: (id: string, r: { topic: string; partition: number; key: string; value: string; headers: { key: string; value: string }[] }) =>
    call<Message>('KafkaProduce', id, r),
  kafkaGroups: (id: string) => call<GroupSummary[] | null>('KafkaGroups', id).then((x) => x ?? []),
  kafkaDescribeGroup: (id: string, g: string) => call<GroupDetail>('KafkaDescribeGroup', id, g),
  kafkaResetOffsets: (id: string, r: { group: string; topic: string; partitions: number[]; mode: string; value: number; dryRun: boolean }) =>
    call<OffsetChange[] | null>('KafkaResetOffsets', id, r).then((x) => x ?? []),
  kafkaDeleteGroup: (id: string, g: string) => call<void>('KafkaDeleteGroup', id, g),
  exportMessages: (name: string, msgs: unknown[]) => call<string>('ExportMessages', name, msgs),
  kafkaSkipPreview: (id: string, r: SkipReq) => call<SkipPreview>('KafkaSkipPreview', id, r),
  kafkaSkip: (id: string, r: SkipReq, reason: string, move: boolean) => call<ParkedBatch>('KafkaSkip', id, r, reason, move),
  parkedBatches: (instanceId: string) => call<ParkedBatch[] | null>('ParkedBatches', instanceId).then((x) => x ?? []),
  parkedMessages: (batchId: string, limit: number) => call<Message[] | null>('ParkedMessages', batchId, limit).then((x) => x ?? []),
  kafkaReplay: (r: { batchId: string; instanceId: string; topic: string; keepPartitions: boolean; force: boolean }) => call<Replay>('KafkaReplay', r),
  deleteParked: (batchId: string) => call<void>('DeleteParked', batchId),
  exportParked: (batchId: string) => call<string>('ExportParked', batchId),

  changesetPlan: (id: string, text: string, env: string) => call<Plan>('ChangesetPlan', id, text, env),
  changesetApply: (id: string, text: string, env: string, fingerprint: string, allowDelete: boolean) =>
    call<ChangeResult[] | null>('ChangesetApply', id, text, env, fingerprint, allowDelete).then((x) => x ?? []),
  changesetExport: (id: string) => call<string>('ChangesetExport', id),
  changesetOpenFile: () => call<TextFile>('ChangesetOpenFile'),
  changesetSaveFile: (path: string, name: string, content: string) => call<string>('ChangesetSaveFile', path, name, content),

  redisOverview: (id: string) => call<Record<string, string>>('RedisOverview', id),
  redisScan: (id: string, pattern: string, cursor: number, count: number) => call<ScanResult>('RedisScan', id, pattern, cursor, count),
  redisGet: (id: string, key: string) => call<KeyValue>('RedisGet', id, key),
  redisSetString: (id: string, key: string, value: string, ttl: number) => call<void>('RedisSetString', id, key, value, ttl),
  redisSetTTL: (id: string, key: string, ttl: number) => call<void>('RedisSetTTL', id, key, ttl),
  redisDelete: (id: string, keys: string[]) => call<number>('RedisDelete', id, keys),
  redisExec: (id: string, line: string, confirmed: boolean) => call<string>('RedisExec', id, line, confirmed),
  redisSlowLog: (id: string, n: number) => call<SlowLog[] | null>('RedisSlowLog', id, n).then((x) => x ?? []),

  rabbitQueues: (id: string) => call<Queue[] | null>('RabbitQueues', id).then((x) => x ?? []),
  rabbitExchanges: (id: string) => call<Exchange[] | null>('RabbitExchanges', id).then((x) => x ?? []),
  rabbitQueueBindings: (id: string, q: string) => call<Binding[] | null>('RabbitQueueBindings', id, q).then((x) => x ?? []),
  rabbitCreateQueue: (id: string, r: { name: string; type: string; durable: boolean; arguments: Record<string, unknown>; bindExchange: string; routingKey: string }) =>
    call<void>('RabbitCreateQueue', id, r),
  rabbitCreateExchange: (id: string, r: { name: string; type: string; durable: boolean }) => call<void>('RabbitCreateExchange', id, r),
  rabbitDeleteQueue: (id: string, q: string) => call<void>('RabbitDeleteQueue', id, q),
  rabbitPurgeQueue: (id: string, q: string) => call<void>('RabbitPurgeQueue', id, q),
  rabbitPeek: (id: string, q: string, n: number) => call<PeekedMessage[] | null>('RabbitPeek', id, q, n).then((x) => x ?? []),
  rabbitPublish: (id: string, r: { exchange: string; routingKey: string; payload: string; headers: Record<string, string>; persistent: boolean }) =>
    call<boolean>('RabbitPublish', id, r),
  rabbitExportDefinitions: (id: string) => call<string>('RabbitExportDefinitions', id),

  changesetLibrary: () => call<LibraryFile[] | null>('ChangesetLibrary').then((x) => x ?? []),
  changesetLibraryRead: (name: string) => call<string>('ChangesetLibraryRead', name),
  changesetLibrarySave: (name: string, content: string, overwrite: boolean) => call<string>('ChangesetLibrarySave', name, content, overwrite),
  changesetLibraryDelete: (name: string) => call<void>('ChangesetLibraryDelete', name),

  teamStatus: () => call<TeamStatus>('TeamStatus'),
  teamPickRepo: () => call<TeamStatus>('TeamPickRepo'),
  teamSetRepo: (dir: string) => call<TeamStatus>('TeamSetRepo', dir),
  teamSync: () => call<TeamStatus>('TeamSync'),
  teamCatalog: () => call<CatalogEntry[] | null>('TeamCatalog').then((x) => x ?? []),
  teamPublishCatalog: (ids: string[]) => call<void>('TeamPublishCatalog', ids),
  teamImportCatalog: (ids: string[]) => call<{ added: number; overwritten: number; skipped: number }>('TeamImportCatalog', ids),

  watchState: () => call<WatchState>('WatchState'),
  watchSaveRule: (r: WatchRule) => call<WatchRule>('WatchSaveRule', r),
  watchDeleteRule: (id: string) => call<void>('WatchDeleteRule', id),
  watchCheck: (id: string) => call<WatchStatus>('WatchCheck', id),
  watchClearAlerts: () => call<void>('WatchClearAlerts'),
  watchTestNotify: () => call<void>('WatchTestNotify'),
}

export const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))
