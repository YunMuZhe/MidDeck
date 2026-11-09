import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, App, Badge, Button, DatePicker, Drawer, Input, InputNumber, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { api, errMsg } from '../api'
import { CopyCommand, num, useDangerConfirm } from '../common'
import { useT } from '../settings'
import type { GroupDetail, GroupSummary, Instance, OffsetChange } from '../types'
import { kafkaCli } from './KafkaView'

const stateBadge: Record<string, 'success' | 'default' | 'warning' | 'error'> = {
  Stable: 'success', Empty: 'default', PreparingRebalance: 'warning', CompletingRebalance: 'warning', Dead: 'error',
}

const LagValue = ({ lag }: { lag: number }) => (
  <span style={{ fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>
    {lag > 0 && <Badge status={lag > 10000 ? 'error' : 'warning'} style={{ marginRight: 6 }} />}
    {num(lag)}
  </span>
)

export default function GroupsTab({ inst, onChanged }: { inst: Instance; onChanged: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [groups, setGroups] = useState<GroupSummary[]>([])
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [detail, setDetail] = useState<GroupDetail | null>(null)
  const confirm = useDangerConfirm(inst)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setGroups(await api.kafkaGroups(inst.id))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [inst.id, message])

  useEffect(() => {
    load()
  }, [load])

  const open = async (g: string) => {
    try {
      setDetail(await api.kafkaDescribeGroup(inst.id, g))
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  const rows = useMemo(
    () => groups.filter((g) => g.group.toLowerCase().includes(q.toLowerCase()) || (g.topics ?? []).some((x) => x.includes(q))),
    [groups, q],
  )

  return (
    <>
      <div className="toolbar">
        <Input prefix={<SearchOutlined className="muted" />} placeholder={t('Filter groups or topics')} allowClear value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 280 }} />
        <div className="spacer" />
        <Button icon={<ReloadOutlined />} onClick={() => { load(); onChanged() }} loading={loading} />
      </div>
      <Table
        size="middle"
        rowKey="group"
        loading={loading}
        dataSource={rows}
        pagination={{ pageSize: 50, hideOnSinglePage: true }}
        onRow={(g) => ({ onClick: () => open(g.group), style: { cursor: 'pointer' } })}
        columns={[
          { title: t('Group'), dataIndex: 'group', render: (g) => <a>{g}</a>, sorter: (a, b) => a.group.localeCompare(b.group) },
          { title: t('State'), dataIndex: 'state', width: 170, render: (s) => <Badge status={stateBadge[s] ?? 'default'} text={s} /> },
          { title: t('Members'), dataIndex: 'members', width: 100, align: 'right' },
          { title: t('Topics'), dataIndex: 'topics', render: (ts: string[] | null) => (ts ?? []).map((x) => <Tag bordered={false} key={x}>{x}</Tag>) },
          { title: t('Lag'), dataIndex: 'lag', width: 130, align: 'right', defaultSortOrder: 'descend', sorter: (a, b) => a.lag - b.lag, render: (l) => <LagValue lag={l} /> },
          {
            title: '', width: 80, align: 'right',
            render: (_, g) => !inst.readOnly && (
              <Typography.Link type="danger" onClick={(e) => {
                e.stopPropagation()
                confirm(t('Delete consumer group'), g.group, async () => {
                  try { await api.kafkaDeleteGroup(inst.id, g.group); load(); onChanged() } catch (err) { message.error(errMsg(err)); throw err }
                })
              }}>{t('Delete')}</Typography.Link>
            ),
          },
        ]}
      />
      {detail && <GroupDrawer inst={inst} detail={detail} onClose={() => setDetail(null)} onRefresh={() => { open(detail.group); load(); onChanged() }} />}
    </>
  )
}

function GroupDrawer({ inst, detail, onClose, onRefresh }: { inst: Instance; detail: GroupDetail; onClose: () => void; onRefresh: () => void }) {
  const t = useT()
  const [resetTopic, setResetTopic] = useState<string | null>(null)
  return (
    <Drawer title={detail.group} width={920} open onClose={onClose} extra={<Button icon={<ReloadOutlined />} onClick={onRefresh} />}>
      <Space size="large" style={{ marginBottom: 16 }}>
        <Badge status={stateBadge[detail.state] ?? 'default'} text={detail.state} />
        <span>{t('Members')}: <b>{detail.members}</b></span>
        <span>{t('Total lag')}: <LagValue lag={detail.lag} /></span>
      </Space>
      {(detail.topics ?? []).map((x) => (
        <div key={x} style={{ marginBottom: 22 }}>
          <div className="toolbar">
            <Typography.Text strong>{x}</Typography.Text>
            <div className="spacer" />
            {!inst.readOnly && <Button size="small" onClick={() => setResetTopic(x)}>{t('Reset offsets')}</Button>}
          </div>
          <Table size="small" rowKey="partition" pagination={false}
            dataSource={(detail.partitions ?? []).filter((p) => p.topic === x)}
            columns={[
              { title: t('Partition'), dataIndex: 'partition', width: 90 },
              { title: t('Committed'), dataIndex: 'committed', align: 'right', render: (c) => (c < 0 ? '-' : num(c)) },
              { title: t('End'), dataIndex: 'end', align: 'right', render: num },
              { title: t('Lag'), dataIndex: 'lag', align: 'right', render: (l) => <LagValue lag={l} /> },
              { title: t('Consumer'), dataIndex: 'member' },
              { title: t('Host'), dataIndex: 'host' },
            ]} />
        </div>
      ))}
      {resetTopic && <ResetModal inst={inst} group={detail} topic={resetTopic} onClose={() => setResetTopic(null)} onDone={onRefresh} />}
    </Drawer>
  )
}

function ResetModal({ inst, group, topic, onClose, onDone }: { inst: Instance; group: GroupDetail; topic: string; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [mode, setMode] = useState('latest')
  const [value, setValue] = useState(0)
  const [time, setTime] = useState<Dayjs>(dayjs().subtract(1, 'hour'))
  const [parts, setParts] = useState<number[]>([])
  const [preview, setPreview] = useState<OffsetChange[] | null>(null)
  const [busy, setBusy] = useState(false)
  const val = mode === 'timestamp' ? time.valueOf() : value
  const active = group.state !== 'Empty' && group.state !== 'Dead'
  const allParts = (group.partitions ?? []).filter((p) => p.topic === topic).map((p) => p.partition)
  const clear = () => setPreview(null)

  const run = async (dryRun: boolean) => {
    setBusy(true)
    try {
      const r = await api.kafkaResetOffsets(inst.id, { group: group.group, topic, partitions: parts, mode, value: val, dryRun })
      setPreview(r)
      if (!dryRun) {
        message.success(t('Offsets reset'))
        onDone()
        onClose()
      }
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={t('Reset offsets: {g} / {t}', { g: group.group, t: topic })} open onCancel={onClose} width={740}
      footer={[
        <Button key="p" onClick={() => run(true)} loading={busy}>{t('Preview')}</Button>,
        <Button key="x" type="primary" danger disabled={!preview || active} onClick={() => run(false)} loading={busy}>{t('Reset')}</Button>,
      ]}>
      <Space direction="vertical" style={{ width: '100%' }} size={12}>
        {active && <Alert type="warning" showIcon message={t('The group is {s}. Kafka only allows resetting offsets when no consumer is running: stop the consumers, refresh, then reset. You can preview now.', { s: group.state })} />}
        <Space wrap>
          <Select value={mode} onChange={(m) => { setMode(m); clear() }} style={{ width: 200 }} options={[
            { value: 'earliest', label: t('Earliest') }, { value: 'latest', label: t('Latest (skip backlog)') },
            { value: 'timestamp', label: t('At time') }, { value: 'offset', label: t('To offset') }, { value: 'shift', label: t('Shift by N') },
          ]} />
          {mode === 'timestamp' && <DatePicker showTime value={time} onChange={(x) => { if (x) { setTime(x); clear() } }} />}
          {(mode === 'offset' || mode === 'shift') && (
            <InputNumber value={value} onChange={(n) => { setValue(n ?? 0); clear() }} addonBefore={mode === 'shift' ? '±' : 'offset'} />
          )}
          <Select mode="multiple" allowClear placeholder={t('All partitions')} style={{ minWidth: 200 }} value={parts}
            onChange={(p) => { setParts(p); clear() }} options={allParts.map((p) => ({ value: p, label: t('Partition {n}', { n: p }) }))} />
        </Space>
        {preview && (
          <Table size="small" rowKey="partition" pagination={false} dataSource={preview}
            columns={[
              { title: t('Partition'), dataIndex: 'partition' },
              { title: t('Current'), dataIndex: 'from', align: 'right', render: (f) => (f < 0 ? '-' : num(f)) },
              { title: t('New'), dataIndex: 'to', align: 'right', render: (x) => <b>{num(x)}</b> },
              {
                title: t('Effect'), align: 'right',
                render: (_, c) => {
                  const d = c.to - Math.max(c.from, 0)
                  return d > 0 ? t('skip {n}', { n: num(d) }) : d < 0 ? t('re-consume {n}', { n: num(-d) }) : t('unchanged')
                },
              },
            ]} />
        )}
        <CopyCommand cmd={kafkaCli.resetOffsets(inst, group.group, topic, mode, val)} />
      </Space>
    </Modal>
  )
}
