import { useCallback, useEffect, useState } from 'react'
import { Alert, App, Button, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tabs, Tag, Tooltip, Typography } from 'antd'
import { BellOutlined, DeleteOutlined, EditOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { api, errMsg, onBackendEvent } from './api'
import { EnvTag, fmtBytes, KindIcon, num } from './common'
import { useT } from './settings'
import type { Instance, Kind, WatchAlert, WatchItem, WatchRule, WatchState, WatchStatus, WatchType } from './types'

const typeLabel: Record<WatchType, string> = { kafka_lag: 'Consumer lag', rabbit_dlq: 'Dead-letter queue', redis_bigkey: 'Big Redis key' }
const kindType: Record<Kind, WatchType> = { kafka: 'kafka_lag', rabbitmq: 'rabbit_dlq', redis: 'redis_bigkey' }
const stateColor: Record<string, string> = { idle: 'default', ok: 'green', firing: 'red', error: 'orange' }
const kindColor: Record<string, string> = { firing: 'red', reminder: 'volcano', recovered: 'green', error: 'orange' }
const kindLabel: Record<string, string> = { firing: 'Firing', reminder: 'Still firing', recovered: 'Recovered', error: 'Check failed' }

const show = (type: WatchType, v: number) => (type === 'redis_bigkey' ? fmtBytes(v) : num(v))

function Items({ type, items, max = 3 }: { type: WatchType; items?: WatchItem[] | null; max?: number }) {
  const t = useT()
  if (!items?.length) return <span className="muted">-</span>
  return (
    <div style={{ fontSize: 12 }}>
      {items.slice(0, max).map((it) => (
        <div key={it.name}>
          <span className="mono">{it.name}</span> <b>{show(type, it.value)}</b>
          {!!it.delta && it.delta > 0 && <span className="muted"> (+{num(it.delta)})</span>}
        </div>
      ))}
      {items.length > max && <div className="muted">{t('and {n} more', { n: items.length - max })}</div>}
    </div>
  )
}

const newRule = (inst?: Instance): WatchRule => ({
  id: '', name: '', instanceId: inst?.id ?? '', type: inst ? kindType[inst.kind] : 'kafka_lag', target: '',
  threshold: inst?.kind === 'redis' ? 10 * 1024 * 1024 : inst?.kind === 'rabbitmq' ? 1 : 10000, growth: 0, intervalSec: inst?.kind === 'redis' ? 300 : 60, enabled: true,
})

/** Patrol alerts: rules checked in the background while MidDeck is open. */
export function AlertsModal({ open, onClose, instances, current, onOpenInstance }: {
  open: boolean; onClose: () => void; instances: Instance[]; current: Instance | null; onOpenInstance: (id: string) => void
}) {
  const { message } = App.useApp()
  const t = useT()
  const [st, setSt] = useState<WatchState>({ rules: [], statuses: [], alerts: [] })
  const [editing, setEditing] = useState<WatchRule | null>(null)
  const [checking, setChecking] = useState('')
  const byId = (id: string) => instances.find((i) => i.id === id)

  const load = useCallback(() => api.watchState().then(setSt).catch((e) => message.error(errMsg(e))), [message])
  useEffect(() => {
    if (!open) return
    load()
    const timer = setInterval(load, 5000)
    return () => clearInterval(timer)
  }, [open, load])

  const rules = st.rules ?? []
  const status = (id: string): WatchStatus | undefined => (st.statuses ?? []).find((s) => s.ruleId === id)

  const act = async (fn: () => Promise<unknown>, ok?: string) => {
    try {
      await fn()
      if (ok) message.success(ok)
    } catch (e) {
      message.error(errMsg(e))
    }
    load()
  }

  const instCell = (id: string) => {
    const i = byId(id)
    return i ? (
      <a onClick={() => { onClose(); onOpenInstance(id) }}><Space size={6}><KindIcon kind={i.kind} size={16} />{i.name}<EnvTag env={i.env} /></Space></a>
    ) : <span className="muted">{t('deleted connection')}</span>
  }

  return (
    <Modal title={t('Alerts')} open={open} onCancel={onClose} footer={null} width={1040}>
      <Tabs
        tabBarExtraContent={
          <Space>
            <Tooltip title={t('Shows a sample notification. If nothing appears, allow notifications for MidDeck (or Script Editor on macOS) in system settings.')}>
              <Button size="small" icon={<BellOutlined />} onClick={() => act(() => api.watchTestNotify(), t('Notification sent'))}>{t('Test notification')}</Button>
            </Tooltip>
            <Button size="small" type="primary" icon={<PlusOutlined />} disabled={!instances.length} onClick={() => setEditing(newRule(current ?? instances[0]))}>{t('New rule')}</Button>
          </Space>
        }
        items={[
          {
            key: 'rules', label: t('Rules'),
            children: (
              <>
                <Table
                  size="small"
                  rowKey="id"
                  dataSource={rules}
                  pagination={{ pageSize: 8, hideOnSinglePage: true }}
                  locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('No rules yet. Add one to watch consumer lag, dead-letter queues or big Redis keys.')} /> }}
                  columns={[
                    { title: '', dataIndex: 'enabled', width: 54, render: (v, r) => <Switch size="small" checked={v} onChange={(on) => act(() => api.watchSaveRule({ ...r, enabled: on }))} /> },
                    { title: t('Rule'), render: (_, r) => <div><div>{r.name || t(typeLabel[r.type])}</div><div className="muted" style={{ fontSize: 12 }}>{r.name ? `${t(typeLabel[r.type])} · ` : ''}<span className="mono">{r.target || t('all')}</span></div></div> },
                    { title: t('Instance'), dataIndex: 'instanceId', render: instCell },
                    {
                      title: t('Fires when'), render: (_, r) => (
                        <div style={{ fontSize: 12 }}>
                          {r.threshold > 0 && <div>≥ {show(r.type, r.threshold)}</div>}
                          {r.growth > 0 && <div>{t('grows by {n} between checks', { n: num(r.growth) })}</div>}
                          <div className="muted">{t('every {n}s', { n: r.intervalSec })}</div>
                        </div>
                      ),
                    },
                    {
                      title: t('Status'), width: 300, render: (_, r) => {
                        const s = status(r.id)
                        if (!r.enabled) return <Tag bordered={false}>{t('off')}</Tag>
                        if (!s || s.state === 'idle') return <Tag bordered={false}>{t('not checked yet')}</Tag>
                        return (
                          <div>
                            <Tag color={stateColor[s.state]}>{t(s.state === 'ok' ? 'OK' : s.state === 'firing' ? 'Firing' : 'Check failed')}</Tag>
                            <span className="muted" style={{ fontSize: 12 }}>{new Date(s.checkedAt).toLocaleTimeString()} · {t('{n} checked', { n: num(s.checked) })}</span>
                            {s.state === 'error' ? <div style={{ fontSize: 12, color: 'var(--ant-color-warning)' }}>{s.error}</div> : <Items type={r.type} items={s.worst} />}
                          </div>
                        )
                      },
                    },
                    {
                      title: '', width: 110, align: 'right', render: (_, r) => (
                        <Space size={2}>
                          <Tooltip title={t('Check now')}><Button type="text" size="small" icon={<PlayCircleOutlined />} loading={checking === r.id} onClick={async () => { setChecking(r.id); await act(() => api.watchCheck(r.id)); setChecking('') }} /></Tooltip>
                          <Button type="text" size="small" icon={<EditOutlined />} onClick={() => setEditing(r)} />
                          <Popconfirm title={t('Delete this rule?')} okText={t('Delete')} cancelText={t('Cancel')} onConfirm={() => act(() => api.watchDeleteRule(r.id))}>
                            <Button type="text" size="small" danger icon={<DeleteOutlined />} />
                          </Popconfirm>
                        </Space>
                      ),
                    },
                  ]}
                />
                <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 10, marginBottom: 0 }}>
                  {t('Rules are checked on this computer while MidDeck is open. Checks only read. A rule notifies when it starts firing, every 30 minutes while it keeps firing, and when it recovers.')}
                </Typography.Paragraph>
              </>
            ),
          },
          {
            key: 'history', label: t('Recent alerts'),
            children: (
              <>
                <Table
                  size="small"
                  rowKey={(a) => a.time + a.ruleId + a.kind}
                  dataSource={st.alerts ?? []}
                  pagination={{ pageSize: 10, hideOnSinglePage: true }}
                  locale={{ emptyText: t('No alerts so far') }}
                  columns={[
                    { title: t('Time'), dataIndex: 'time', width: 170, render: (v) => new Date(v).toLocaleString() },
                    { title: '', dataIndex: 'kind', width: 110, render: (k) => <Tag color={kindColor[k]}>{t(kindLabel[k] ?? k)}</Tag> },
                    { title: t('Rule'), render: (_, a: WatchAlert) => a.rule || t(typeLabel[a.type]) },
                    { title: t('Instance'), dataIndex: 'instance', render: instCell },
                    { title: t('Details'), render: (_, a: WatchAlert) => (a.kind === 'error' ? <span style={{ fontSize: 12 }}>{a.error}</span> : <Items type={a.type} items={a.items} />) },
                  ]}
                />
                {!!st.alerts?.length && <Button size="small" style={{ marginTop: 8 }} onClick={() => act(() => api.watchClearAlerts())}>{t('Clear history')}</Button>}
              </>
            ),
          },
        ]}
      />
      <RuleForm rule={editing} instances={instances} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); load() }} />
    </Modal>
  )
}

const MB = 1024 * 1024

function RuleForm({ rule, instances, onClose, onSaved }: { rule: WatchRule | null; instances: Instance[]; onClose: () => void; onSaved: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [r, setR] = useState<WatchRule>(newRule())
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (rule) setR(rule)
  }, [rule])
  const set = (p: Partial<WatchRule>) => setR((x) => ({ ...x, ...p }))
  const redis = r.type === 'redis_bigkey'

  const pickInstance = (id: string) => {
    const inst = instances.find((i) => i.id === id)
    if (!inst) return
    const type = kindType[inst.kind]
    // Thresholds mean different things per type, so switching kinds starts from that type's defaults.
    set(type === r.type ? { instanceId: id } : { ...newRule(inst), id: r.id, name: r.name })
  }

  const save = async () => {
    setBusy(true)
    try {
      await api.watchSaveRule(r)
      onSaved()
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  const targetHelp: Record<WatchType, string> = {
    kafka_lag: 'Consumer group name or pattern, e.g. order-*. Empty watches every group.',
    rabbit_dlq: 'Queue name or pattern, e.g. *.dlq. Empty watches the queues that look like dead-letter queues (named dlq / dead, or used as a dead-letter routing key).',
    redis_bigkey: 'Key pattern, e.g. session:*. Empty samples the whole keyspace.',
  }

  return (
    <Modal
      title={r.id ? t('Edit rule') : t('New rule')}
      open={!!rule}
      onCancel={onClose}
      onOk={save}
      okText={t('Save')}
      cancelText={t('Cancel')}
      confirmLoading={busy}
      okButtonProps={{ disabled: !r.instanceId || (r.threshold <= 0 && r.growth <= 0) }}
      width={560}
    >
      <Form layout="vertical" style={{ marginTop: 12 }}>
        <Form.Item label={t('Instance')} style={{ marginBottom: 12 }}>
          <Select
            showSearch
            optionFilterProp="search"
            value={r.instanceId || undefined}
            onChange={pickInstance}
            options={instances.map((i) => ({ value: i.id, search: `${i.name} ${i.project} ${i.env}`, label: <Space size={6}><KindIcon kind={i.kind} size={16} />{i.name}<EnvTag env={i.env} /></Space> }))}
          />
        </Form.Item>
        <Form.Item label={t('Watches')} style={{ marginBottom: 12 }}>
          <Tag bordered={false} color="blue">{t(typeLabel[r.type])}</Tag>
          <span className="muted" style={{ fontSize: 12 }}>{t('decided by the connection type')}</span>
        </Form.Item>
        <Form.Item label={t('Target')} extra={t(targetHelp[r.type])} style={{ marginBottom: 12 }}>
          <Input className="mono" value={r.target} onChange={(e) => set({ target: e.target.value })} placeholder={t('all')} />
        </Form.Item>
        <Space size={16} align="start" wrap>
          <Form.Item label={redis ? t('Key size at least') : r.type === 'kafka_lag' ? t('Lag at least') : t('Messages at least')} style={{ marginBottom: 12 }}>
            {redis ? (
              <InputNumber min={0.01} step={1} value={+(r.threshold / MB).toFixed(2)} onChange={(v) => set({ threshold: Math.round((v ?? 0) * MB) })} addonAfter="MB" style={{ width: 160 }} />
            ) : (
              <InputNumber min={0} value={r.threshold} onChange={(v) => set({ threshold: v ?? 0 })} style={{ width: 160 }} />
            )}
          </Form.Item>
          {!redis && (
            <Form.Item label={t('Or grows by at least')} tooltip={t('Compared with the previous check. 0 turns it off.')} style={{ marginBottom: 12 }}>
              <InputNumber min={0} value={r.growth} onChange={(v) => set({ growth: v ?? 0 })} style={{ width: 160 }} />
            </Form.Item>
          )}
          <Form.Item label={t('Check every')} style={{ marginBottom: 12 }}>
            <InputNumber min={redis ? 60 : 15} value={r.intervalSec} onChange={(v) => set({ intervalSec: v ?? 60 })} addonAfter={t('seconds')} style={{ width: 170 }} />
          </Form.Item>
        </Space>
        {redis && <Alert type="info" showIcon style={{ marginBottom: 12 }} message={t('Each check samples about 5,000 keys with SCAN and MEMORY USAGE and continues where the last one stopped, so a large keyspace is covered over several checks.')} />}
        <Form.Item label={t('Name (optional)')} style={{ marginBottom: 0 }}>
          <Input value={r.name} onChange={(e) => set({ name: e.target.value })} placeholder={t(typeLabel[r.type])} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

/** Shows an in-app notice for each alert and reports how many rules are firing. Mounted once. */
export function useAlerts(onOpen: () => void): number {
  const { notification } = App.useApp()
  const [firing, setFiring] = useState(0)
  useEffect(() => {
    const refresh = () => api.watchState().then((s) => setFiring((s.statuses ?? []).filter((x) => x.state === 'firing').length)).catch(() => {})
    refresh()
    const timer = setInterval(refresh, 30000)
    const off = onBackendEvent<{ alert: WatchAlert; title: string; body: string }>('watch:alert', (e) => {
      refresh()
      const fn = e.alert.kind === 'recovered' ? notification.success : e.alert.kind === 'error' ? notification.warning : notification.error
      fn({ message: e.title, description: e.body, placement: 'bottomRight', duration: e.alert.kind === 'recovered' ? 6 : 0, onClick: onOpen, style: { cursor: 'pointer' } })
    })
    return () => {
      clearInterval(timer)
      off()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return firing
}
