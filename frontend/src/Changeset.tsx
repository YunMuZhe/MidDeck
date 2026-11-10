import { useEffect, useMemo, useState } from 'react'
import { Alert, App, AutoComplete, Button, Checkbox, Dropdown, Empty, Input, Modal, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { CheckCircleFilled, CloseCircleFilled, CloudDownloadOutlined, DownOutlined, FolderOpenOutlined, MinusCircleFilled, SaveOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { api, errMsg } from './api'
import { CopyCommand, EnvTag, isProd } from './common'
import { useT } from './settings'
import type { Change, ChangeResult, Instance, Plan } from './types'

const templates: Record<string, string> = {
  kafka: `# MidDeck changeset: describe topics once, plan and apply them to any cluster.
version: 1
kafka:
  defaults:
    replication: 3
    configs:
      min.insync.replicas: "2"
  topics:
    - name: order-events
      partitions: 12
      configs:
        retention.ms: "604800000"   # 7 days
    - name: order-refund
      partitions: 6
    # - name: legacy-topic
    #   state: absent             # deleting needs "Allow deletions"

# Per-environment overrides, matched against the connection's environment.
envs:
  dev:
    kafka:
      defaults:
        replication: 1
        configs: {min.insync.replicas: "1"}
`,
  rabbitmq: `# MidDeck changeset: describe exchanges, queues and bindings, plan and apply them to any vhost.
version: 1
rabbitmq:
  exchanges:
    - name: orders
      type: topic
  queues:
    - name: order-refund
      type: quorum
      arguments:
        x-delivery-limit: 5
    - name: order-mail
      arguments:
        x-message-ttl: 60000
  bindings:
    - exchange: orders
      queue: order-refund
      routingKey: order.refund.*
    - exchange: orders
      queue: order-mail
      routingKey: order.created
`,
}

const actionColor: Record<string, string> = { create: 'green', update: 'blue', delete: 'red', conflict: 'orange' }
const sign: Record<string, string> = { create: '+', update: '~', delete: '−', conflict: '!' }

/** YAML changeset editor: load / export, plan against this instance, apply. */
export function ChangesetModal({ inst, instances, open, onClose }: { inst: Instance; instances: Instance[]; open: boolean; onClose: () => void }) {
  const { message, modal } = App.useApp()
  const t = useT()
  const [text, setText] = useState('')
  const [file, setFile] = useState<{ path: string; name: string } | null>(null)
  const [env, setEnv] = useState(inst.env)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [planned, setPlanned] = useState('') // text + env the plan was made from
  const [results, setResults] = useState<ChangeResult[] | null>(null)
  const [busy, setBusy] = useState<'' | 'plan' | 'apply' | 'load'>('')
  const [error, setError] = useState('')
  const [allowDelete, setAllowDelete] = useState(false)
  const [cli, setCli] = useState('MidDeck')

  useEffect(() => {
    if (!open) return
    setEnv(inst.env)
    setPlan(null)
    setResults(null)
    setError('')
    setText((x) => x || templates[inst.kind] || '')
    api.mcpInfo().then((i) => setCli(i.command)).catch(() => {})
  }, [open, inst])

  const stale = !!plan && planned !== env + '\n' + text
  const peers = instances.filter((i) => i.kind === inst.kind && i.id !== inst.id)
  const envOptions = useMemo(() => {
    const found = new Set<string>(['dev', 'test', 'staging', 'prod'])
    if (inst.env) found.add(inst.env)
    for (const m of text.matchAll(/^ {2}([A-Za-z0-9_-]+):\s*$/gm)) {
      const before = text.slice(0, m.index)
      if (/^envs:\s*$/m.test(before)) found.add(m[1])
    }
    return [...found].map((value) => ({ value }))
  }, [text, inst.env])

  const run = async <T,>(kind: typeof busy, fn: () => Promise<T>) => {
    setBusy(kind)
    setError('')
    try {
      return await fn()
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setBusy('')
    }
  }

  const doPlan = () =>
    run('plan', async () => {
      setResults(null)
      const p = await api.changesetPlan(inst.id, text, env)
      setPlan(p)
      setPlanned(env + '\n' + text)
    })

  const doApply = () => {
    if (!plan) return
    const prod = isProd(inst.env)
    let typed = ''
    modal.confirm({
      title: t('Apply {n} change(s) to {name}?', { n: plan.changes.filter((c) => c.action !== 'conflict').length, name: inst.name }),
      okType: 'danger',
      okText: t('Apply'),
      cancelText: t('Cancel'),
      content: (
        <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
          <Space size={6}><span className="muted">{t('Environment')}</span> <EnvTag env={inst.env} /> {env !== inst.env && <span className="muted">({t('overrides for {e}', { e: env })})</span>}</Space>
          {plan.changes.some((c) => c.action === 'conflict') && <Alert type="warning" showIcon message={t('Conflicting changes are skipped.')} />}
          {prod && (
            <>
              <Alert type="error" showIcon message={t('This is production. Type the name to confirm.')} />
              <Input placeholder={inst.name} onChange={(e) => (typed = e.target.value)} />
            </>
          )}
        </Space>
      ),
      onOk: () => {
        if (prod && typed !== inst.name) return Promise.reject(new Error('name mismatch'))
        return run('apply', async () => {
          const r = await api.changesetApply(inst.id, text, env, plan.fingerprint, allowDelete)
          setResults(r)
          const failed = r.filter((x) => x.status === 'failed').length
          if (failed) message.error(t('{n} change(s) failed', { n: failed }))
          else message.success(t('Changeset applied'))
        })
      },
    })
  }

  const openFile = () =>
    run('load', async () => {
      const f = await api.changesetOpenFile()
      if (!f.path) return
      setText(f.content)
      setFile({ path: f.path, name: f.name })
      setPlan(null)
    })

  const save = async (as: boolean) => {
    try {
      const p = await api.changesetSaveFile(as ? '' : file?.path ?? '', file?.name ?? `${inst.name}-changeset`, text)
      if (p) {
        setFile({ path: p, name: p.split(/[\\/]/).pop() ?? p })
        message.success(t('Saved to {p}', { p }))
      }
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  const load = (id: string) =>
    run('load', async () => {
      const y = await api.changesetExport(id)
      setText(y)
      setFile(null)
      setPlan(null)
    })

  const deletes = plan?.changes.filter((c) => c.action === 'delete').length ?? 0
  const applicable = plan ? plan.changes.filter((c) => c.action !== 'conflict').length : 0
  const shownFile = file?.path ?? '<file>.yaml'
  const ref = /\s/.test(inst.name) ? `"${inst.name}"` : inst.name

  return (
    <Modal
      title={<Space>{t('Changeset')}<Typography.Text type="secondary" style={{ fontWeight: 400 }}>{inst.name}</Typography.Text><EnvTag env={inst.env} /></Space>}
      open={open}
      onCancel={onClose}
      width={1180}
      footer={null}
      destroyOnClose={false}
      className="changeset-modal"
    >
      <div className="changeset">
        <div className="changeset-editor">
          <div className="toolbar" style={{ marginBottom: 8 }}>
            <Button size="small" icon={<FolderOpenOutlined />} onClick={openFile}>{t('Open')}</Button>
            <Button size="small" icon={<SaveOutlined />} onClick={() => save(false)} disabled={!text}>{t('Save')}</Button>
            <Dropdown
              trigger={['click']}
              menu={{
                onClick: ({ key }) => load(key),
                items: [
                  { type: 'group', label: t('This connection'), children: [{ key: inst.id, label: inst.name }] },
                  ...(peers.length
                    ? [{ type: 'group' as const, label: t('Other {kind} connections', { kind: inst.kind === 'kafka' ? 'Kafka' : 'RabbitMQ' }), children: peers.map((p) => ({ key: p.id, label: `${p.name}${p.env ? ` · ${p.env}` : ''}` })) }]
                    : []),
                ],
              }}
            >
              <Button size="small" icon={<CloudDownloadOutlined />} loading={busy === 'load'}>{t('Load from a connection…')} <DownOutlined /></Button>
            </Dropdown>
            <div className="spacer" />
            <Typography.Text type="secondary" className="mono" style={{ fontSize: 12 }} ellipsis>{file?.name ?? t('unsaved')}</Typography.Text>
          </div>
          <Input.TextArea
            className="mono changeset-text"
            value={text}
            onChange={(e) => setText(e.target.value)}
            spellCheck={false}
            autoSize={false}
          />
          <div className="muted" style={{ fontSize: 12, marginTop: 6 }}>
            {t('Loading another connection and planning here promotes its topology to this one. Only what the file lists is touched.')}
          </div>
        </div>

        <div className="changeset-plan">
          <div className="toolbar" style={{ marginBottom: 8 }}>
            <Tooltip title={t('Selects the envs.<env> overrides')}>
              <AutoComplete size="small" style={{ width: 150 }} value={env} onChange={setEnv} options={envOptions}>
                <Input size="small" prefix={<span className="muted">env</span>} />
              </AutoComplete>
            </Tooltip>
            <Button size="small" type="primary" ghost icon={<ThunderboltOutlined />} onClick={doPlan} loading={busy === 'plan'} disabled={!text.trim()}>{t('Plan')}</Button>
            <div className="spacer" />
            {deletes > 0 && <Checkbox checked={allowDelete} onChange={(e) => setAllowDelete(e.target.checked)}>{t('Allow deletions')}</Checkbox>}
            <Button size="small" type="primary" danger={isProd(inst.env)} onClick={doApply} loading={busy === 'apply'}
              disabled={!plan || stale || applicable === 0 || inst.readOnly || (deletes > 0 && !allowDelete) || !!results}>
              {t('Apply')}
            </Button>
          </div>
          {error && <Alert type="error" showIcon message={<span style={{ whiteSpace: 'pre-wrap' }}>{error}</span>} style={{ marginBottom: 10 }} />}
          {inst.readOnly && <Alert type="info" showIcon message={t('This connection is read-only: you can plan, not apply.')} style={{ marginBottom: 10 }} />}
          {stale && <Alert type="warning" showIcon message={t('The YAML or environment changed since this plan. Plan again before applying.')} style={{ marginBottom: 10 }} />}
          {busy === 'plan' || busy === 'load' ? (
            <div style={{ padding: 60, textAlign: 'center' }}><Spin /></div>
          ) : plan ? (
            <PlanView plan={plan} results={results} />
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('Plan to see what would change. Nothing runs until you apply.')} style={{ marginTop: 60 }} />
          )}
          <div style={{ marginTop: 'auto', paddingTop: 12 }}>
            <CopyCommand title={t('Same thing from a terminal or CI')} cmd={`${cli} changeset plan -f ${shownFile} -i ${ref}${env && env !== inst.env ? ` --env ${env}` : ''}`} />
          </div>
        </div>
      </div>
    </Modal>
  )
}

function PlanView({ plan, results }: { plan: Plan; results: ChangeResult[] | null }) {
  const t = useT()
  const count = (a: string) => plan.changes.filter((c) => c.action === a).length
  const byKey = new Map((results ?? []).map((r) => [r.resource + '/' + r.name, r]))
  return (
    <div className="plan-view">
      <Space wrap size={[6, 6]} style={{ marginBottom: 10 }}>
        <Tag color="green">{t('{n} to create', { n: count('create') })}</Tag>
        <Tag color="blue">{t('{n} to update', { n: count('update') })}</Tag>
        <Tag color="red">{t('{n} to delete', { n: count('delete') })}</Tag>
        {count('conflict') > 0 && <Tag color="orange">{t('{n} conflict(s)', { n: count('conflict') })}</Tag>}
        <Tag>{t('{n} unchanged', { n: plan.unchanged })}</Tag>
        {plan.env && <span className="muted" style={{ fontSize: 12 }}>env {plan.env}</span>}
      </Space>
      {plan.changes.length === 0 && <Alert type="success" showIcon message={t('No changes. The connection already matches the changeset.')} />}
      {plan.changes.map((c) => <ChangeRow key={c.resource + '/' + c.name} c={c} result={byKey.get(c.resource + '/' + c.name)} />)}
      {plan.warnings.map((w) => <Alert key={w} type="warning" showIcon message={w} style={{ marginTop: 8 }} />)}
    </div>
  )
}

function ChangeRow({ c, result }: { c: Change; result?: ChangeResult }) {
  const t = useT()
  return (
    <div className={`plan-change ${c.action}`}>
      <div className="plan-change-head">
        <span className="plan-sign">{sign[c.action]}</span>
        <Tag bordered={false} color={actionColor[c.action]}>{t(c.action)}</Tag>
        <span className="muted">{t(c.resource)}</span>
        <b className="mono">{c.name}</b>
        <div className="spacer" />
        {result?.status === 'applied' && <CheckCircleFilled style={{ color: '#2f9e44' }} />}
        {result?.status === 'failed' && <Tooltip title={result.error}><CloseCircleFilled style={{ color: '#e03131' }} /></Tooltip>}
        {result?.status === 'skipped' && <Tooltip title={t('Skipped')}><MinusCircleFilled className="muted" /></Tooltip>}
      </div>
      {(c.diffs ?? []).map((d) => (
        <div key={d.field} className="plan-diff mono">
          <span className="muted">{d.field}:</span>{' '}
          {c.action === 'create' ? d.to : <><span className="plan-from">{d.from || '—'}</span> → <span className="plan-to">{d.to || t('(default)')}</span></>}
        </div>
      ))}
      {(c.notes ?? []).map((n) => <div key={n} className="plan-note">{n}</div>)}
      {result?.error && <div className="plan-note" style={{ color: '#e03131' }}>{result.error}</div>}
    </div>
  )
}
