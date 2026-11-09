import { useCallback, useEffect, useMemo, useState } from 'react'
import { App, Button, Checkbox, Col, Descriptions, Drawer, Form, Input, InputNumber, Modal, Row, Space, Table, Tabs, Tag, Typography } from 'antd'
import { MinusCircleOutlined, PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { api, errMsg } from '../api'
import { CopyCommand, num, useDangerConfirm } from '../common'
import { useT } from '../settings'
import type { ActionArgs, Instance, Topic, TopicDetail } from '../types'
import { kafkaCli } from './KafkaView'

const commonConfigs = ['retention.ms', 'cleanup.policy', 'min.insync.replicas', 'max.message.bytes', 'retention.bytes', 'compression.type']

export default function TopicsTab({ inst, prefill, onBrowse, onChanged }: { inst: Instance; prefill?: ActionArgs; onBrowse: (t: string) => void; onChanged: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [topics, setTopics] = useState<Topic[]>([])
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [showInternal, setShowInternal] = useState(false)
  const [detail, setDetail] = useState<TopicDetail | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const confirm = useDangerConfirm(inst)
  useEffect(() => {
    if (prefill) setCreateOpen(true)
  }, [prefill])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setTopics(await api.kafkaTopics(inst.id))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [inst.id, message])

  useEffect(() => {
    load()
  }, [load])

  const rows = useMemo(
    () => topics.filter((x) => (showInternal || !x.internal) && x.name.toLowerCase().includes(q.toLowerCase())),
    [topics, q, showInternal],
  )

  const openDetail = async (name: string) => {
    try {
      setDetail(await api.kafkaDescribeTopic(inst.id, name))
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  const changed = () => {
    load()
    onChanged()
  }

  return (
    <>
      <div className="toolbar">
        <Input prefix={<SearchOutlined className="muted" />} placeholder={t('Filter topics')} allowClear value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 280 }} />
        <Checkbox checked={showInternal} onChange={(e) => setShowInternal(e.target.checked)}>{t('Show internal')}</Checkbox>
        <div className="spacer" />
        <span className="muted">{t('{n} topics', { n: rows.length })}</span>
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading} />
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)} disabled={inst.readOnly}>{t('New topic')}</Button>
      </div>
      <Table
        size="middle"
        rowKey="name"
        loading={loading}
        dataSource={rows}
        pagination={{ pageSize: 50, hideOnSinglePage: true }}
        columns={[
          {
            title: t('Topic'), dataIndex: 'name', sorter: (a, b) => a.name.localeCompare(b.name),
            render: (n, x) => (
              <Space>
                <a onClick={() => openDetail(n)}>{n}</a>
                {x.internal && <Tag bordered={false}>internal</Tag>}
                {x.error && <Tag color="error" title={x.error}>error</Tag>}
              </Space>
            ),
          },
          { title: t('Partitions'), dataIndex: 'partitions', width: 120, align: 'right', sorter: (a, b) => a.partitions - b.partitions },
          { title: t('Replicas'), dataIndex: 'replication', width: 100, align: 'right' },
          {
            title: '', width: 220, align: 'right',
            render: (_, x) => (
              <Space size="middle">
                <a onClick={() => onBrowse(x.name)}>{t('Messages')}</a>
                <a onClick={() => openDetail(x.name)}>{t('Details')}</a>
                {!inst.readOnly && !x.internal && (
                  <Typography.Link type="danger" onClick={() =>
                    confirm(t('Delete topic (cannot be undone)'), x.name, async () => {
                      try {
                        await api.kafkaDeleteTopic(inst.id, x.name)
                        message.success(t('Deleted'))
                        changed()
                      } catch (e) {
                        message.error(errMsg(e))
                        throw e
                      }
                    })}>
                    {t('Delete')}
                  </Typography.Link>
                )}
              </Space>
            ),
          },
        ]}
      />
      <CreateTopicModal inst={inst} initial={prefill} open={createOpen} onClose={() => setCreateOpen(false)} onDone={changed} />
      <TopicDrawer inst={inst} detail={detail} onClose={() => setDetail(null)} onChanged={(n) => { openDetail(n); changed() }} />
    </>
  )
}

function CreateTopicModal({ inst, initial, open, onClose, onDone }: { inst: Instance; initial?: ActionArgs; open: boolean; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  useEffect(() => {
    if (!open || !initial) return
    const configs = []
    if (initial.retention_ms) configs.push({ key: 'retention.ms', value: String(initial.retention_ms) })
    if (initial.cleanup) configs.push({ key: 'cleanup.policy', value: initial.cleanup })
    form.setFieldsValue({
      name: initial.name, configs,
      ...(initial.partitions ? { partitions: initial.partitions } : {}),
      ...(initial.replication ? { replication: initial.replication } : {}),
    })
  }, [open, initial, form])
  const [busy, setBusy] = useState(false)
  const v = Form.useWatch([], form) ?? {}
  const cfg: Record<string, string> = {}
  for (const c of v.configs ?? []) if (c?.key && c?.value) cfg[c.key] = c.value

  const submit = async () => {
    const values = await form.validateFields()
    setBusy(true)
    try {
      await api.kafkaCreateTopic(inst.id, { name: values.name, partitions: values.partitions, replication: values.replication, configs: cfg })
      message.success(t('Created {name}', { name: values.name }))
      form.resetFields()
      onClose()
      onDone()
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={t('New topic')} open={open} onCancel={onClose} onOk={submit} okText={t('Create')} cancelText={t('Cancel')} confirmLoading={busy} width={640} destroyOnClose>
      <Form form={form} layout="vertical" requiredMark={false} initialValues={{ partitions: 3, replication: 1, configs: [] }}>
        <Form.Item name="name" label={t('Name')} rules={[{ required: true, message: t('Required') }, { pattern: /^[a-zA-Z0-9._-]+$/, message: t('Letters, digits, . _ - only') }]}>
          <Input placeholder="order-events" />
        </Form.Item>
        <Row gutter={12}>
          <Col span={12}><Form.Item name="partitions" label={t('Partitions')} rules={[{ required: true }]}><InputNumber min={1} max={10000} style={{ width: '100%' }} /></Form.Item></Col>
          <Col span={12}><Form.Item name="replication" label={t('Replication factor')} rules={[{ required: true }]}><InputNumber min={1} max={10} style={{ width: '100%' }} /></Form.Item></Col>
        </Row>
        <div className="muted" style={{ marginBottom: 6 }}>{t('Configs')}</div>
        <Form.List name="configs">
          {(fields, { add, remove }) => (
            <>
              {fields.map((f) => (
                <Space key={f.key} align="baseline" style={{ display: 'flex' }}>
                  <Form.Item name={[f.name, 'key']} style={{ width: 240 }}><Input placeholder="retention.ms" list="kafka-cfg-keys" /></Form.Item>
                  <Form.Item name={[f.name, 'value']} style={{ width: 260 }}><Input placeholder="604800000" /></Form.Item>
                  <MinusCircleOutlined onClick={() => remove(f.name)} />
                </Space>
              ))}
              <Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => add()}>{t('Add config')}</Button>
              <datalist id="kafka-cfg-keys">{commonConfigs.map((c) => <option key={c} value={c} />)}</datalist>
            </>
          )}
        </Form.List>
      </Form>
      <CopyCommand cmd={kafkaCli.createTopic(inst, v.name ?? '', v.partitions ?? 3, v.replication ?? 1, cfg)} />
    </Modal>
  )
}

function TopicDrawer({ inst, detail, onClose, onChanged }: { inst: Instance; detail: TopicDetail | null; onClose: () => void; onChanged: (n: string) => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [parts, setParts] = useState<number | null>(null)
  const [editKey, setEditKey] = useState('')
  const [editVal, setEditVal] = useState('')
  if (!detail) return null
  return (
    <Drawer title={detail.name} width={860} open onClose={onClose}>
      <Descriptions size="small" bordered column={3}>
        <Descriptions.Item label={t('Partitions')}>{detail.partitions}</Descriptions.Item>
        <Descriptions.Item label={t('Replicas')}>{detail.replication}</Descriptions.Item>
        <Descriptions.Item label={t('Messages (approx.)')}>{num(detail.messages)}</Descriptions.Item>
      </Descriptions>
      <Tabs
        style={{ marginTop: 12 }}
        items={[
          {
            key: 'p', label: t('Partitions'),
            children: (
              <>
                {!inst.readOnly && (
                  <div className="toolbar">
                    <span>{t('Grow to')}</span>
                    <InputNumber min={detail.partitions + 1} value={parts ?? undefined} onChange={(n) => setParts(n)} />
                    <span>{t('partitions')}</span>
                    <Button disabled={!parts} onClick={async () => {
                      try {
                        await api.kafkaSetPartitions(inst.id, detail.name, parts!)
                        message.success(t('Done'))
                        onChanged(detail.name)
                      } catch (e) { message.error(errMsg(e)) }
                    }}>{t('Apply')}</Button>
                    <span className="muted">{t('Partitions can only grow; keyed routing will change.')}</span>
                  </div>
                )}
                <Table size="small" rowKey="id" pagination={false} dataSource={detail.partitionList}
                  columns={[
                    { title: '#', dataIndex: 'id', width: 60 },
                    { title: t('Leader'), dataIndex: 'leader', width: 80 },
                    { title: t('Replicas'), dataIndex: 'replicas', render: (r: number[]) => r?.join(', ') },
                    {
                      title: 'ISR', dataIndex: 'isr',
                      render: (isr: number[], p) => (isr?.length ?? 0) < (p.replicas?.length ?? 0)
                        ? <Tag color="error">{t('Under-replicated')}: {isr?.join(', ')}</Tag> : isr?.join(', '),
                    },
                    { title: t('Start offset'), dataIndex: 'startOffset', align: 'right', render: num },
                    { title: t('End offset'), dataIndex: 'endOffset', align: 'right', render: num },
                    { title: t('Messages'), align: 'right', render: (_, p) => num(p.endOffset - p.startOffset) },
                  ]} />
              </>
            ),
          },
          {
            key: 'c', label: t('Configs'),
            children: (
              <>
                {!inst.readOnly && (
                  <div className="toolbar">
                    <Input placeholder={t('Config key')} value={editKey} onChange={(e) => setEditKey(e.target.value)} style={{ width: 220 }} list="kafka-cfg-keys2" />
                    <Input placeholder={t('Value (empty = reset to default)')} value={editVal} onChange={(e) => setEditVal(e.target.value)} style={{ width: 240 }} />
                    <Button disabled={!editKey} onClick={async () => {
                      try {
                        await api.kafkaAlterTopicConfigs(inst.id, detail.name, { [editKey]: editVal })
                        message.success(t('Done'))
                        onChanged(detail.name)
                      } catch (e) { message.error(errMsg(e)) }
                    }}>{t('Apply')}</Button>
                    <datalist id="kafka-cfg-keys2">{commonConfigs.map((c) => <option key={c} value={c} />)}</datalist>
                  </div>
                )}
                <Table size="small" rowKey="key" pagination={false} dataSource={detail.configs ?? []}
                  onRow={(r) => ({ onClick: () => { setEditKey(r.key); setEditVal(r.value) }, style: { cursor: 'pointer' } })}
                  columns={[
                    { title: t('Key'), dataIndex: 'key', render: (k, r) => <span style={{ fontWeight: r.source.includes('DYNAMIC_TOPIC') ? 600 : 400 }}>{k}</span> },
                    { title: t('Value'), dataIndex: 'value', render: (v, r) => (r.sensitive ? '******' : <span className="mono">{v}</span>) },
                    { title: t('Source'), dataIndex: 'source', render: (s) => <Tag bordered={false}>{s}</Tag> },
                  ]} />
              </>
            ),
          },
        ]}
      />
    </Drawer>
  )
}
