import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, App, Button, DatePicker, Drawer, Input, InputNumber, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { api, errMsg } from '../api'
import { CopyCommand, useDangerConfirm } from '../common'
import type { GroupDetail, GroupSummary, Instance, OffsetChange } from '../types'
import { kafkaCli } from './KafkaView'

const stateColor: Record<string, string> = { Stable: 'green', Empty: 'default', PreparingRebalance: 'orange', CompletingRebalance: 'orange', Dead: 'red' }

export default function GroupsTab({ inst }: { inst: Instance }) {
  const { message } = App.useApp()
  const [groups, setGroups] = useState<GroupSummary[]>([])
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [detail, setDetail] = useState<GroupDetail | null>(null)
  const { confirm, ctx } = useDangerConfirm(inst)

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
    () => groups.filter((g) => g.group.toLowerCase().includes(q.toLowerCase()) || (g.topics ?? []).some((t) => t.includes(q))),
    [groups, q],
  )

  return (
    <>
      {ctx}
      <Space style={{ marginBottom: 12 }}>
        <Input.Search placeholder="过滤消费组 / topic" allowClear value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 280 }} />
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
      </Space>
      <Table
        size="small"
        rowKey="group"
        loading={loading}
        dataSource={rows}
        pagination={{ pageSize: 50 }}
        columns={[
          { title: '消费组', dataIndex: 'group', render: (g) => <a onClick={() => open(g)}>{g}</a>, sorter: (a, b) => a.group.localeCompare(b.group) },
          { title: '状态', dataIndex: 'state', width: 160, render: (s) => <Tag color={stateColor[s]}>{s}</Tag> },
          { title: '成员', dataIndex: 'members', width: 80 },
          { title: 'Topic', dataIndex: 'topics', render: (ts: string[] | null) => (ts ?? []).map((t) => <Tag key={t}>{t}</Tag>) },
          {
            title: '总 Lag', dataIndex: 'lag', width: 120, defaultSortOrder: 'descend', sorter: (a, b) => a.lag - b.lag,
            render: (l) => <span style={{ color: l > 10000 ? '#e03131' : l > 0 ? '#f08c00' : undefined, fontWeight: 600 }}>{l.toLocaleString()}</span>,
          },
          {
            title: '操作', width: 80,
            render: (_, g) => !inst.readOnly && (
              <a style={{ color: '#e03131' }} onClick={() => confirm('删除消费组', g.group, async () => {
                try { await api.kafkaDeleteGroup(inst.id, g.group); load() } catch (e) { message.error(errMsg(e)); throw e }
              })}>删除</a>
            ),
          },
        ]}
      />
      {detail && <GroupDrawer inst={inst} detail={detail} onClose={() => setDetail(null)} onRefresh={() => open(detail.group)} />}
    </>
  )
}

function GroupDrawer({ inst, detail, onClose, onRefresh }: { inst: Instance; detail: GroupDetail; onClose: () => void; onRefresh: () => void }) {
  const [resetTopic, setResetTopic] = useState<string | null>(null)
  return (
    <Drawer title={detail.group} width={900} open onClose={onClose}
      extra={<Space><Button icon={<ReloadOutlined />} onClick={onRefresh}>刷新</Button></Space>}>
      <Space style={{ marginBottom: 12 }}>
        <Tag color={stateColor[detail.state]}>{detail.state}</Tag>
        <span>成员 {detail.members}</span>
        <span>总 Lag <b>{detail.lag.toLocaleString()}</b></span>
      </Space>
      {(detail.topics ?? []).map((t) => (
        <div key={t} style={{ marginBottom: 20 }}>
          <Space style={{ marginBottom: 6 }}>
            <Typography.Text strong>{t}</Typography.Text>
            {!inst.readOnly && <Button size="small" onClick={() => setResetTopic(t)}>重置 offset</Button>}
          </Space>
          <Table size="small" rowKey="partition" pagination={false}
            dataSource={(detail.partitions ?? []).filter((p) => p.topic === t)}
            columns={[
              { title: '分区', dataIndex: 'partition', width: 60 },
              { title: '已提交', dataIndex: 'committed', render: (c) => (c < 0 ? '-' : c) },
              { title: '末尾', dataIndex: 'end' },
              { title: 'Lag', dataIndex: 'lag', render: (l) => <b style={{ color: l > 0 ? '#f08c00' : undefined }}>{l}</b> },
              { title: '消费者', dataIndex: 'member' },
              { title: '主机', dataIndex: 'host' },
            ]} />
        </div>
      ))}
      {resetTopic && <ResetModal inst={inst} group={detail} topic={resetTopic} onClose={() => setResetTopic(null)} onDone={onRefresh} />}
    </Drawer>
  )
}

function ResetModal({ inst, group, topic, onClose, onDone }: { inst: Instance; group: GroupDetail; topic: string; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const [mode, setMode] = useState('latest')
  const [value, setValue] = useState(0)
  const [time, setTime] = useState<Dayjs>(dayjs().subtract(1, 'hour'))
  const [parts, setParts] = useState<number[]>([])
  const [preview, setPreview] = useState<OffsetChange[] | null>(null)
  const [busy, setBusy] = useState(false)
  const val = mode === 'timestamp' ? time.valueOf() : value
  const active = group.state !== 'Empty' && group.state !== 'Dead'
  const allParts = (group.partitions ?? []).filter((p) => p.topic === topic).map((p) => p.partition)

  const run = async (dryRun: boolean) => {
    setBusy(true)
    try {
      const r = await api.kafkaResetOffsets(inst.id, { group: group.group, topic, partitions: parts, mode, value: val, dryRun })
      setPreview(r)
      if (!dryRun) {
        message.success('offset 已重置')
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
    <Modal title={`重置 offset：${group.group} / ${topic}`} open onCancel={onClose} width={720}
      footer={[
        <Button key="p" onClick={() => run(true)} loading={busy}>预览</Button>,
        <Button key="x" type="primary" danger disabled={!preview || active} onClick={() => run(false)} loading={busy}>执行重置</Button>,
      ]}>
      <Space direction="vertical" style={{ width: '100%' }}>
        {active && <Alert type="warning" showIcon message={`消费组状态为 ${group.state}：Kafka 只允许在没有活跃消费者时重置 offset。请先停止消费者，再刷新后执行。可以先预览。`} />}
        <Space wrap>
          <Select value={mode} onChange={(m) => { setMode(m); setPreview(null) }} style={{ width: 160 }} options={[
            { value: 'earliest', label: '最早' }, { value: 'latest', label: '最新（跳过积压）' },
            { value: 'timestamp', label: '指定时间' }, { value: 'offset', label: '指定 offset' }, { value: 'shift', label: '偏移 N 条' },
          ]} />
          {mode === 'timestamp' && <DatePicker showTime value={time} onChange={(t) => { if (t) { setTime(t); setPreview(null) } }} />}
          {(mode === 'offset' || mode === 'shift') && (
            <InputNumber value={value} onChange={(n) => { setValue(n ?? 0); setPreview(null) }} addonBefore={mode === 'shift' ? '±' : 'offset'} />
          )}
          <Select mode="multiple" allowClear placeholder="全部分区" style={{ minWidth: 200 }} value={parts}
            onChange={(p) => { setParts(p); setPreview(null) }} options={allParts.map((p) => ({ value: p, label: `分区 ${p}` }))} />
        </Space>
        {preview && (
          <Table size="small" rowKey="partition" pagination={false} dataSource={preview}
            columns={[
              { title: '分区', dataIndex: 'partition' },
              { title: '当前', dataIndex: 'from', render: (f) => (f < 0 ? '-' : f) },
              { title: '重置为', dataIndex: 'to', render: (t) => <b>{t}</b> },
              { title: '变化', render: (_, c) => { const d = c.to - Math.max(c.from, 0); return <span style={{ color: d < 0 ? '#1971c2' : '#e8590c' }}>{d > 0 ? `跳过 ${d}` : d < 0 ? `重新消费 ${-d}` : '不变'}</span> } },
            ]} />
        )}
        <CopyCommand cmd={kafkaCli.resetOffsets(inst, group.group, topic, mode, val)} />
      </Space>
    </Modal>
  )
}
