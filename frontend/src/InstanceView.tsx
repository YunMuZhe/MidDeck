import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Drawer, Dropdown, Result, Space, Spin, Tooltip } from 'antd'
import {
  ApiOutlined, DeleteOutlined, DiffOutlined, EditOutlined, LockOutlined, MedicineBoxOutlined, MoreOutlined, ReloadOutlined, RightOutlined,
} from '@ant-design/icons'
import { api, errMsg } from './api'
import { ChangesetModal, type ChangesetDraft } from './Changeset'
import { CompatBadge, DiagReport, EnvTag, isProd, KindIcon, useDangerConfirm } from './common'
import KafkaView from './kafka/KafkaView'
import RedisView from './redis/RedisView'
import RabbitView from './rabbitmq/RabbitView'
import { useT } from './settings'
import { kindLabel } from './theme'
import type { ConnectResult, Instance, Intent, Report } from './types'

interface Props {
  inst: Instance
  instances: Instance[]
  onEdit: () => void
  onDeleted: () => void
  onConnected: (on: boolean) => void
  intent?: Intent
  onIntentDone: () => void
}

export default function InstanceView({ inst, instances, onEdit, onDeleted, onConnected, intent, onIntentDone }: Props) {
  const { message } = App.useApp()
  const t = useT()
  const [state, setState] = useState<'connecting' | 'ok' | 'fail'>('connecting')
  const [conn, setConn] = useState<ConnectResult | null>(null)
  const [report, setReport] = useState<Report | null>(null)
  const [error, setError] = useState('')
  const [showReport, setShowReport] = useState(false)
  const [diagnosing, setDiagnosing] = useState(false)
  const [changeset, setChangeset] = useState(false)
  const [draft, setDraft] = useState<ChangesetDraft | null>(null)

  // The assistant's changeset draft opens the editor; planning and applying stay with the user.
  useEffect(() => {
    if (intent?.action !== 'changeset_draft') return
    setDraft({ seq: intent.seq, text: String(intent.args.yaml ?? '') })
    setChangeset(true)
    onIntentDone()
  }, [intent]) // eslint-disable-line react-hooks/exhaustive-deps
  const confirm = useDangerConfirm(inst)
  const onConnectedRef = useRef(onConnected)
  onConnectedRef.current = onConnected

  const connect = useCallback(async (fresh = false) => {
    setState('connecting')
    setError('')
    try {
      if (fresh) await api.disconnect(inst.id)
      const r = await api.connect(inst.id)
      setConn(r)
      if (r.report) setReport(r.report)
      setState('ok')
      onConnectedRef.current(true)
    } catch (e) {
      setError(errMsg(e))
      setState('fail')
      onConnectedRef.current(false)
    }
  }, [inst.id])

  useEffect(() => {
    connect()
  }, [connect])

  const diagnose = async () => {
    setShowReport(true)
    setDiagnosing(true)
    try {
      setReport(await api.testConnection(await api.getInstance(inst.id)))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setDiagnosing(false)
    }
  }

  const prod = isProd(inst.env)
  const compat = conn?.info.compat
  const warnCompat = compat && ['partial', 'unsupported', 'unknown'].includes(compat.level)
  const hops = inst.network?.ssh?.length ?? 0
  const [bastionNames, setBastionNames] = useState<Record<string, string>>({})
  useEffect(() => {
    if (inst.network?.ssh?.some((h) => h.bastionId)) {
      api.listBastions().then((l) => setBastionNames(Object.fromEntries(l.map((b) => [b.id, b.name])))).catch(() => {})
    }
  }, [inst])

  return (
    <div style={{ minHeight: '100%' }}>
      {prod && <div className="prod-bar" />}
      <div className="inst-head drag">
        <div className="crumbs">
          <span>{inst.project || t('Ungrouped')}</span>
          <RightOutlined style={{ fontSize: 9 }} />
          <EnvTag env={inst.env} />
        </div>
        <div className="inst-title">
          <KindIcon kind={inst.kind} size={34} />
          <h1>{inst.name}</h1>
          {inst.readOnly && <Tooltip title={t('Write operations are refused')}><span className="env-pill other"><LockOutlined /> {t('Read-only')}</span></Tooltip>}
          {state === 'ok' && <CompatBadge c={compat} />}
          <div style={{ flex: 1 }} />
          <Space className="no-drag">
            {state === 'ok' && inst.kind !== 'redis' && (
              <Tooltip title={t('Describe topology as YAML, plan and apply it')}>
                <Button icon={<DiffOutlined />} onClick={() => setChangeset(true)}>{t('Changeset')}</Button>
              </Tooltip>
            )}
            <Button icon={<MedicineBoxOutlined />} onClick={() => (report ? setShowReport(true) : diagnose())}>{t('Diagnostics')}</Button>
            <Button icon={<ReloadOutlined />} onClick={() => connect(true)}>{t('Reconnect')}</Button>
            <Button icon={<EditOutlined />} onClick={onEdit}>{t('Edit')}</Button>
            <Dropdown
              trigger={['click']}
              menu={{
                items: [{ key: 'del', danger: true, icon: <DeleteOutlined />, label: t('Delete connection') }],
                onClick: () =>
                  confirm(t('Delete connection'), inst.name, async () => {
                    await api.deleteInstance(inst.id)
                    onDeleted()
                  }),
              }}
            >
              <Button icon={<MoreOutlined />} />
            </Dropdown>
          </Space>
        </div>
        <div className="inst-sub">
          <span>{kindLabel[inst.kind]}</span>
          <span className="mono selectable">{(inst.addrs ?? []).join(', ') || inst.mgmtUrl}</span>
          {hops > 0 && (
            <span>
              <ApiOutlined /> {t('via {n} SSH hop(s)', { n: hops })}: {inst.network.ssh.map((h) => (h.bastionId ? bastionNames[h.bastionId] ?? '?' : h.host)).join(' → ')}
            </span>
          )}
        </div>
        {state === 'ok' && compat && warnCompat && (
          <Alert
            style={{ marginTop: 14 }}
            type={compat.level === 'unsupported' ? 'error' : 'warning'}
            showIcon
            closable
            message={compat.message}
            description={compat.unavailable?.length ? `${t('Unavailable')}: ${compat.unavailable.join(', ')}` : undefined}
          />
        )}
        {state === 'ok' && compat && !warnCompat && !!compat.unavailable?.length && (
          <Alert style={{ marginTop: 14 }} type="info" showIcon closable message={`${t('Not available on this version')}: ${compat.unavailable.join(', ')}`} />
        )}
      </div>

      <div className="main-inner">
        {state === 'connecting' && (
          <div style={{ padding: '120px 0', textAlign: 'center' }}>
            <Spin />
            <div className="muted" style={{ marginTop: 12 }}>{hops ? t('Opening SSH tunnel and connecting…') : t('Connecting…')}</div>
          </div>
        )}
        {state === 'fail' && (
          <Result
            status="warning"
            title={t('Could not connect')}
            subTitle={error}
            extra={
              <Space>
                <Button type="primary" onClick={diagnose}>{t('Run diagnostics')}</Button>
                <Button onClick={() => connect(true)}>{t('Retry')}</Button>
                <Button onClick={onEdit}>{t('Edit settings')}</Button>
              </Space>
            }
          />
        )}
        {state === 'ok' && inst.kind === 'kafka' && <KafkaView inst={inst} instances={instances} intent={intent} onIntentDone={onIntentDone} />}
        {state === 'ok' && inst.kind === 'redis' && <RedisView inst={inst} intent={intent} onIntentDone={onIntentDone} />}
        {state === 'ok' && inst.kind === 'rabbitmq' && <RabbitView inst={inst} intent={intent} onIntentDone={onIntentDone} />}
      </div>

      {inst.kind !== 'redis' && <ChangesetModal inst={inst} instances={instances} open={changeset} onClose={() => setChangeset(false)} draft={draft} />}
      <Drawer
        title={t('Connection diagnostics')}
        width={620}
        open={showReport}
        onClose={() => setShowReport(false)}
        extra={<Button size="small" icon={<ReloadOutlined />} loading={diagnosing} onClick={diagnose}>{t('Run again')}</Button>}
      >
        {diagnosing ? (
          <div style={{ textAlign: 'center', padding: 40 }}>
            <Spin />
            <div className="muted" style={{ marginTop: 12 }}>{t('Checking network → auth → version → permissions…')}</div>
          </div>
        ) : (
          <DiagReport report={report} />
        )}
      </Drawer>
    </div>
  )
}
