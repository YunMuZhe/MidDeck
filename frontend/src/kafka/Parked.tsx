import { useCallback, useEffect, useState } from 'react'
import { Alert, App, AutoComplete, Button, Checkbox, Drawer, Empty, Input, InputNumber, Modal, Select, Space, Table, Tag, Tooltip, Typography } from 'antd'
import { DeleteOutlined, ExportOutlined, InboxOutlined, ReloadOutlined, RetweetOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { api, errMsg } from '../api'
import { EnvTag, fmtBytes, fmtTime, isProd, num, prettyJSON } from '../common'
import { useT } from '../settings'
import type { GroupDetail, Instance, Message, ParkedBatch, SkipPreview } from '../types'

const fmtRange = (r: { partition: number; from: number; to: number }) => (r.to - r.from === 1 ? `P${r.partition} @${r.from}` : `P${r.partition} ${r.from}–${r.to - 1}`)

const replayHeaders = ['x-middeck-replay', 'x-middeck-origin', 'x-middeck-replayed-at']

function MessagesTable({ msgs, loading }: { msgs: Message[]; loading?: boolean }) {
  const t = useT()
  return (
    <Table
      size="small"
      rowKey={(m) => `${m.partition}-${m.offset}`}
      loading={loading}
      dataSource={msgs}
      pagination={{ pageSize: 20, hideOnSinglePage: true }}
      expandable={{
        expandRowByClick: true,
        expandedRowRender: (m) => (
          <Space direction="vertical" style={{ width: '100%' }}>
            {!!m.headers?.length && <div>{m.headers.map((h) => <Tag key={h.key} bordered={false}>{h.key}: {h.value}</Tag>)}</div>}
            <div className="code-block"><pre className="mono" style={{ margin: 0 }}>{prettyJSON(m.value)}</pre></div>
          </Space>
        ),
      }}
      columns={[
        { title: t('Partition'), dataIndex: 'partition', width: 80 },
        { title: 'Offset', dataIndex: 'offset', width: 90 },
        { title: t('Time'), dataIndex: 'timestamp', width: 160, render: (x) => <span style={{ whiteSpace: 'nowrap' }}>{fmtTime(x)}</span> },
        { title: 'Key', dataIndex: 'key', width: 140, render: (k) => <span className="mono cell-ellipsis" style={{ maxWidth: 130 }}>{k}</span> },
        { title: 'Value', dataIndex: 'value', render: (v) => <span className="mono cell-ellipsis">{v}</span> },
      ]}
    />
  )
}

/** Export the messages a group is stuck on, then move its offsets past them. */
export function SkipModal({ inst, group, topic, onClose, onDone }: { inst: Instance; group: GroupDetail; topic: string; onClose: () => void; onDone: (b: ParkedBatch) => void }) {
  const { message, modal } = App.useApp()
  const t = useT()
  const parts = (group.partitions ?? []).filter((p) => p.topic === topic)
  const [counts, setCounts] = useState<Record<number, number>>(() => Object.fromEntries(parts.map((p) => [p.partition, p.lag > 0 ? 1 : 0])))
  const [reason, setReason] = useState('')
  const [preview, setPreview] = useState<SkipPreview | null>(null)
  const [busy, setBusy] = useState<'' | 'preview' | 'save' | 'skip'>('')
  const active = group.state !== 'Empty' && group.state !== 'Dead'
  const req = { group: group.group, topic, counts: Object.fromEntries(Object.entries(counts).filter(([, n]) => n > 0)) }
  const total = Object.values(req.counts).reduce((s, n) => s + n, 0)

  const setCount = (p: number, n: number) => {
    setCounts((c) => ({ ...c, [p]: n }))
    setPreview(null)
  }

  const doPreview = async () => {
    setBusy('preview')
    try {
      setPreview(await api.kafkaSkipPreview(inst.id, req))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy('')
    }
  }

  const go = async (move: boolean) => {
    const exec = async () => {
      setBusy(move ? 'skip' : 'save')
      try {
        const b = await api.kafkaSkip(inst.id, req, reason, move)
        message.success(move ? t('Parked {n} message(s) and moved the group past them', { n: b.count }) : t('Saved {n} message(s); offsets unchanged', { n: b.count }))
        onDone(b)
      } catch (e) {
        message.error(errMsg(e))
        throw e
      } finally {
        setBusy('')
      }
    }
    if (!move) return exec()
    const prod = isProd(inst.env)
    let typed = ''
    modal.confirm({
      title: t('Skip {n} message(s) for {g}?', { n: preview?.total ?? total, g: group.group }),
      okType: 'danger',
      okText: t('Save & skip'),
      cancelText: t('Cancel'),
      content: (
        <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
          <span>{t('The messages are saved to MidDeck first; only then the committed offsets move. You can replay them later from the Parked tab.')}</span>
          {prod && (
            <>
              <Alert type="error" showIcon message={t('This is production. Type the name to confirm.')} />
              <Input placeholder={group.group} onChange={(e) => (typed = e.target.value)} />
            </>
          )}
        </Space>
      ),
      onOk: () => (prod && typed !== group.group ? Promise.reject(new Error('name mismatch')) : exec()),
    })
  }

  return (
    <Modal
      title={t('Skip stuck messages: {g} / {t}', { g: group.group, t: topic })}
      open
      onCancel={onClose}
      width={900}
      footer={[
        <Button key="p" onClick={doPreview} loading={busy === 'preview'} disabled={total === 0}>{t('Preview')}</Button>,
        <Tooltip key="s" title={t('Write the messages to a file without moving offsets')}>
          <Button onClick={() => go(false)} loading={busy === 'save'} disabled={!preview}>{t('Save only')}</Button>
        </Tooltip>,
        <Button key="x" type="primary" danger onClick={() => go(true)} loading={busy === 'skip'} disabled={!preview || active || inst.readOnly}>{t('Save & skip')}</Button>,
      ]}
    >
      <Space direction="vertical" style={{ width: '100%' }} size={12}>
        {active && <Alert type="warning" showIcon message={t('The group is {s}. Stop its consumers before skipping; you can preview and save now.', { s: group.state })} />}
        <Table
          size="small"
          rowKey="partition"
          pagination={false}
          dataSource={parts}
          columns={[
            { title: t('Partition'), dataIndex: 'partition', width: 90 },
            { title: t('Committed'), dataIndex: 'committed', align: 'right', render: (c) => (c < 0 ? '-' : num(c)) },
            { title: t('End'), dataIndex: 'end', align: 'right', render: num },
            { title: t('Lag'), dataIndex: 'lag', align: 'right', render: num },
            {
              title: t('Skip next'), width: 160, align: 'right',
              render: (_, p) => (
                <InputNumber size="small" min={0} max={Math.max(p.lag, 0)} value={counts[p.partition] ?? 0} disabled={p.committed < 0 || p.lag <= 0}
                  onChange={(n) => setCount(p.partition, n ?? 0)} addonAfter={t('msgs')} style={{ width: 130 }} />
              ),
            },
          ]}
        />
        <Input placeholder={t('Reason (optional), e.g. malformed payload from order-service 2.3.1')} value={reason} onChange={(e) => setReason(e.target.value)} />
        {preview && (
          <>
            <Typography.Text type="secondary">
              {t('{n} message(s) will be parked', { n: preview.total })}: {preview.ranges.map(fmtRange).join(', ')}
              {preview.total > preview.messages.length && ` · ${t('showing the first {n}', { n: preview.messages.length })}`}
            </Typography.Text>
            <MessagesTable msgs={preview.messages} />
          </>
        )}
      </Space>
    </Modal>
  )
}

/** Parked batches for this instance, with replay. */
export default function ParkedTab({ inst, instances, focus }: { inst: Instance; instances: Instance[]; focus?: string }) {
  const { message, modal } = App.useApp()
  const t = useT()
  const [rows, setRows] = useState<ParkedBatch[]>([])
  const [loading, setLoading] = useState(false)
  const [view, setView] = useState<ParkedBatch | null>(null)
  const [replay, setReplay] = useState<ParkedBatch | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setRows(await api.parkedBatches(inst.id))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [inst.id, message])

  useEffect(() => {
    load()
  }, [load, focus])

  const remove = (b: ParkedBatch) =>
    modal.confirm({
      title: t('Delete parked batch {id}?', { id: b.id }),
      content: b.replays.length ? undefined : t('It has not been replayed. The {n} message(s) in it will be gone for good.', { n: b.count }),
      okType: 'danger',
      okText: t('Delete'),
      cancelText: t('Cancel'),
      onOk: async () => {
        await api.deleteParked(b.id)
        load()
      },
    })

  return (
    <>
      <div className="toolbar">
        <Typography.Text type="secondary">{t('Messages skipped past are kept here until you replay or delete them.')}</Typography.Text>
        <div className="spacer" />
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading} />
      </div>
      <Table
        size="middle"
        rowKey="id"
        loading={loading}
        dataSource={rows}
        rowClassName={(b) => (b.id === focus ? 'row-focus' : '')}
        pagination={{ pageSize: 30, hideOnSinglePage: true }}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('Nothing parked. Use "Skip messages" on a consumer group.')} /> }}
        columns={[
          { title: t('Parked at'), dataIndex: 'createdAt', width: 170, render: (x) => <span style={{ whiteSpace: 'nowrap' }}>{dayjs(x).format('YYYY-MM-DD HH:mm:ss')}</span> },
          { title: t('Group'), dataIndex: 'group', render: (g, b) => <div><div>{g}</div>{b.reason && <Typography.Text type="secondary" ellipsis={{ tooltip: b.reason }} style={{ fontSize: 12, maxWidth: 260 }}>{b.reason}</Typography.Text>}</div> },
          { title: t('Topic'), dataIndex: 'topic' },
          { title: t('Messages'), dataIndex: 'count', width: 100, align: 'right', render: (n, b) => <Tooltip title={b.ranges.map(fmtRange).join(', ')}>{num(n)}</Tooltip> },
          { title: t('Size'), dataIndex: 'bytes', width: 90, align: 'right', render: (n) => <span style={{ whiteSpace: 'nowrap' }}>{fmtBytes(n)}</span> },
          {
            title: t('Status'), width: 170, render: (_, b) => {
              const last = b.replays[b.replays.length - 1]
              if (last && !last.error) return <Tooltip title={`${dayjs(last.at).format('MM-DD HH:mm')} → ${last.instanceName} / ${last.topic}`}><Tag color="green" bordered={false}>{t('Replayed')}{b.replays.length > 1 ? ` ×${b.replays.length}` : ''}</Tag></Tooltip>
              if (last?.error) return <Tooltip title={last.error}><Tag color="red" bordered={false}>{t('Replay failed')}</Tag></Tooltip>
              return b.skipped ? <Tag color="orange" bordered={false}>{t('Skipped, pending')}</Tag> : <Tag bordered={false}>{t('Saved only')}</Tag>
            },
          },
          {
            title: '', width: 190, align: 'right', render: (_, b) => (
              <Space size={0}>
                <Button type="link" size="small" onClick={() => setView(b)}>{t('View')}</Button>
                <Button type="link" size="small" icon={<RetweetOutlined />} onClick={() => setReplay(b)}>{t('Replay')}</Button>
                <Tooltip title={t('Export JSONL')}>
                  <Button type="text" size="small" icon={<ExportOutlined />} onClick={async () => {
                    try { const p = await api.exportParked(b.id); if (p) message.success(t('Saved to {p}', { p })) } catch (e) { message.error(errMsg(e)) }
                  }} />
                </Tooltip>
                <Button type="text" size="small" danger icon={<DeleteOutlined />} onClick={() => remove(b)} />
              </Space>
            ),
          },
        ]}
      />
      {view && <BatchDrawer b={view} onClose={() => setView(null)} />}
      {replay && <ReplayModal b={replay} inst={inst} instances={instances} onClose={() => setReplay(null)} onDone={() => { setReplay(null); load() }} />}
    </>
  )
}

function BatchDrawer({ b, onClose }: { b: ParkedBatch; onClose: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [msgs, setMsgs] = useState<Message[]>([])
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    api.parkedMessages(b.id, 500).then(setMsgs).catch((e) => message.error(errMsg(e))).finally(() => setLoading(false))
  }, [b.id, message])
  return (
    <Drawer title={<Space><InboxOutlined />{b.topic}<span className="muted" style={{ fontWeight: 400 }}>{b.id}</span></Space>} width={980} open onClose={onClose}>
      <Space size="large" wrap style={{ marginBottom: 12 }}>
        <span>{t('Group')}: <b>{b.group}</b></span>
        <span>{t('Messages')}: <b>{num(b.count)}</b></span>
        <span className="muted mono selectable" style={{ fontSize: 12 }}>{b.dir}</span>
      </Space>
      {b.reason && <Alert type="info" message={b.reason} style={{ marginBottom: 12 }} />}
      {b.replays.length > 0 && (
        <Table size="small" rowKey="at" pagination={false} style={{ marginBottom: 16 }} dataSource={b.replays}
          columns={[
            { title: t('Replayed at'), dataIndex: 'at', render: (x) => dayjs(x).format('YYYY-MM-DD HH:mm:ss') },
            { title: t('Target'), render: (_, r) => `${r.instanceName} / ${r.topic}` },
            { title: t('Partitions'), dataIndex: 'keepPartitions', render: (k) => (k ? t('original') : t('by key')) },
            { title: t('Messages'), dataIndex: 'count', align: 'right', render: num },
            { title: '', dataIndex: 'error', render: (e) => e && <Typography.Text type="danger">{e}</Typography.Text> },
          ]} />
      )}
      <MessagesTable msgs={msgs} loading={loading} />
    </Drawer>
  )
}

function ReplayModal({ b, inst, instances, onClose, onDone }: { b: ParkedBatch; inst: Instance; instances: Instance[]; onClose: () => void; onDone: () => void }) {
  const { message, modal } = App.useApp()
  const t = useT()
  const [target, setTarget] = useState(b.instanceId || inst.id)
  const [topic, setTopic] = useState(b.topic)
  const [keep, setKeep] = useState(true)
  const [topics, setTopics] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const kafkas = instances.filter((i) => i.kind === 'kafka')
  const tgt = kafkas.find((i) => i.id === target)
  const sameTopic = target === b.instanceId && topic === b.topic

  useEffect(() => {
    api.kafkaTopics(target).then((ts) => setTopics(ts.filter((x) => !x.internal).map((x) => x.name))).catch(() => setTopics([]))
  }, [target])

  const run = async (force: boolean) => {
    setBusy(true)
    try {
      const r = await api.kafkaReplay({ batchId: b.id, instanceId: target, topic, keepPartitions: keep, force })
      message.success(t('Replayed {n} message(s) to {t}', { n: r.count, t: topic }))
      onDone()
    } catch (e) {
      const msg = errMsg(e)
      if (!force && b.replays.some((r) => !r.error)) {
        modal.confirm({ title: t('Replay again?'), content: msg, okText: t('Replay again'), cancelText: t('Cancel'), onOk: () => run(true) })
      } else {
        message.error(msg)
      }
    } finally {
      setBusy(false)
    }
  }

  const confirmRun = () => {
    if (!tgt || !isProd(tgt.env)) return run(false)
    let typed = ''
    modal.confirm({
      title: t('Replay {n} message(s) to production?', { n: b.count }),
      okType: 'danger', okText: t('Replay'), cancelText: t('Cancel'),
      content: (
        <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
          <Alert type="error" showIcon message={t('This is production. Type the name to confirm.')} />
          <Input placeholder={topic} onChange={(e) => (typed = e.target.value)} />
        </Space>
      ),
      onOk: () => (typed !== topic ? Promise.reject(new Error('name mismatch')) : run(false)),
    })
  }

  return (
    <Modal title={t('Replay {n} message(s)', { n: b.count })} open onCancel={onClose} width={640}
      footer={[
        <Button key="c" onClick={onClose}>{t('Cancel')}</Button>,
        <Button key="r" type="primary" icon={<RetweetOutlined />} loading={busy} disabled={!topic || tgt?.readOnly} onClick={confirmRun}>{t('Replay')}</Button>,
      ]}>
      <Space direction="vertical" style={{ width: '100%' }} size={14}>
        {b.replays.some((r) => !r.error) && <Alert type="warning" showIcon message={t('This batch was replayed before. Replaying again produces duplicates.')} />}
        <div>
          <div className="muted" style={{ marginBottom: 4 }}>{t('Target connection')}</div>
          <Select style={{ width: '100%' }} value={target} onChange={setTarget}
            options={kafkas.map((i) => ({ value: i.id, label: <Space>{i.name}<EnvTag env={i.env} />{i.readOnly && <Tag bordered={false}>{t('Read-only')}</Tag>}</Space> }))} />
        </div>
        <div>
          <div className="muted" style={{ marginBottom: 4 }}>{t('Topic')}</div>
          <AutoComplete style={{ width: '100%' }} value={topic} onChange={setTopic} options={topics.map((x) => ({ value: x }))}
            filterOption={(i, o) => !!o?.value.toLowerCase().includes(i.toLowerCase())} />
          {!sameTopic && <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>{t('e.g. a retry topic, or a staging cluster to reproduce the bug')}</div>}
        </div>
        <Checkbox checked={keep} onChange={(e) => setKeep(e.target.checked)}>
          {t('Keep original partitions (preserves per-key order)')}
        </Checkbox>
        <div>
          <div className="muted" style={{ marginBottom: 4 }}>{t('Every message keeps its key, value and headers, and gets:')}</div>
          <Space wrap size={[4, 4]}>{replayHeaders.map((h) => <Tag key={h} bordered={false} className="mono">{h}</Tag>)}</Space>
          <div className="muted" style={{ fontSize: 12, marginTop: 6 }}>{t('Consumers can check x-middeck-replay to treat replays idempotently.')}</div>
        </div>
      </Space>
    </Modal>
  )
}
