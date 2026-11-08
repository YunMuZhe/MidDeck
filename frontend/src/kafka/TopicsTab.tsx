import { useCallback, useEffect, useMemo, useState } from 'react'
import { App, Button, Checkbox, Col, Descriptions, Drawer, Form, Input, InputNumber, Modal, Row, Space, Table, Tabs, Tag, Typography } from 'antd'
import { DeleteOutlined, MinusCircleOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { api, errMsg } from '../api'
import { CopyCommand, useDangerConfirm } from '../common'
import type { Instance, Topic, TopicDetail } from '../types'
import { kafkaCli } from './KafkaView'

const commonConfigs = ['retention.ms', 'cleanup.policy', 'min.insync.replicas', 'max.message.bytes', 'retention.bytes', 'compression.type']

export default function TopicsTab({ inst, onBrowse }: { inst: Instance; onBrowse: (t: string) => void }) {
  const { message } = App.useApp()
  const [topics, setTopics] = useState<Topic[]>([])
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [showInternal, setShowInternal] = useState(false)
  const [detail, setDetail] = useState<TopicDetail | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const { confirm, ctx } = useDangerConfirm(inst)

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
    () => topics.filter((t) => (showInternal || !t.internal) && t.name.toLowerCase().includes(q.toLowerCase())),
    [topics, q, showInternal],
  )

  const openDetail = async (name: string) => {
    try {
      setDetail(await api.kafkaDescribeTopic(inst.id, name))
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  return (
    <>
      {ctx}
      <Space style={{ marginBottom: 12 }} wrap>
        <Input.Search placeholder="过滤 topic" allowClear value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 280 }} />
        <Checkbox checked={showInternal} onChange={(e) => setShowInternal(e.target.checked)}>显示内部 topic</Checkbox>
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)} disabled={inst.readOnly}>新建 Topic</Button>
        <Typography.Text type="secondary">共 {rows.length} 个</Typography.Text>
      </Space>
      <Table
        size="small"
        rowKey="name"
        loading={loading}
        dataSource={rows}
        pagination={{ pageSize: 50, showSizeChanger: true }}
        columns={[
          {
            title: 'Topic', dataIndex: 'name', sorter: (a, b) => a.name.localeCompare(b.name),
            render: (n, t) => (
              <Space>
                <a onClick={() => openDetail(n)}>{n}</a>
                {t.internal && <Tag>internal</Tag>}
                {t.error && <Tag color="red" title={t.error}>error</Tag>}
              </Space>
            ),
          },
          { title: '分区', dataIndex: 'partitions', width: 90, sorter: (a, b) => a.partitions - b.partitions },
          { title: '副本', dataIndex: 'replication', width: 90 },
          {
            title: '操作', width: 200,
            render: (_, t) => (
              <Space>
                <a onClick={() => onBrowse(t.name)}>查消息</a>
                <a onClick={() => openDetail(t.name)}>详情</a>
                {!inst.readOnly && !t.internal && (
                  <a style={{ color: '#e03131' }} onClick={() =>
                    confirm('删除 Topic（不可恢复）', t.name, async () => {
                      try {
                        await api.kafkaDeleteTopic(inst.id, t.name)
                        message.success('已删除')
                        load()
                      } catch (e) {
                        message.error(errMsg(e))
                        throw e
                      }
                    })}>
                    <DeleteOutlined /> 删除
                  </a>
                )}
              </Space>
            ),
          },
        ]}
      />
      <CreateTopicModal inst={inst} open={createOpen} onClose={() => setCreateOpen(false)} onDone={load} />
      <TopicDrawer inst={inst} detail={detail} onClose={() => setDetail(null)} onChanged={(n) => openDetail(n)} />
    </>
  )
}

function CreateTopicModal({ inst, open, onClose, onDone }: { inst: Instance; open: boolean; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const v = Form.useWatch([], form) ?? {}
  const cfg: Record<string, string> = {}
  for (const c of v.configs ?? []) if (c?.key && c?.value) cfg[c.key] = c.value

  const submit = async () => {
    const values = await form.validateFields()
    setBusy(true)
    try {
      await api.kafkaCreateTopic(inst.id, { name: values.name, partitions: values.partitions, replication: values.replication, configs: cfg })
      message.success(`已创建 ${values.name}`)
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
    <Modal title="新建 Topic" open={open} onCancel={onClose} onOk={submit} confirmLoading={busy} width={640} destroyOnClose>
      <Form form={form} layout="vertical" initialValues={{ partitions: 3, replication: 3, configs: [] }}>
        <Form.Item name="name" label="名称" rules={[{ required: true }, { pattern: /^[a-zA-Z0-9._-]+$/, message: '只能包含字母、数字、. _ -' }]}>
          <Input placeholder="order-events" />
        </Form.Item>
        <Row gutter={12}>
          <Col span={12}><Form.Item name="partitions" label="分区数" rules={[{ required: true }]}><InputNumber min={1} max={10000} style={{ width: '100%' }} /></Form.Item></Col>
          <Col span={12}><Form.Item name="replication" label="副本数" rules={[{ required: true }]}><InputNumber min={1} max={10} style={{ width: '100%' }} /></Form.Item></Col>
        </Row>
        <Typography.Text>配置项</Typography.Text>
        <Form.List name="configs">
          {(fields, { add, remove }) => (
            <>
              {fields.map((f) => (
                <Space key={f.key} align="baseline" style={{ display: 'flex' }}>
                  <Form.Item name={[f.name, 'key']} style={{ width: 240 }}>
                    <Input placeholder="retention.ms" list="kafka-cfg-keys" />
                  </Form.Item>
                  <Form.Item name={[f.name, 'value']} style={{ width: 260 }}><Input placeholder="604800000" /></Form.Item>
                  <MinusCircleOutlined onClick={() => remove(f.name)} />
                </Space>
              ))}
              <Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => add()} style={{ margin: '8px 0 16px' }}>添加配置</Button>
              <datalist id="kafka-cfg-keys">{commonConfigs.map((c) => <option key={c} value={c} />)}</datalist>
            </>
          )}
        </Form.List>
      </Form>
      <CopyCommand cmd={kafkaCli.createTopic(inst, v.name ?? '', v.partitions ?? 3, v.replication ?? 3, cfg)} />
    </Modal>
  )
}

function TopicDrawer({ inst, detail, onClose, onChanged }: { inst: Instance; detail: TopicDetail | null; onClose: () => void; onChanged: (n: string) => void }) {
  const { message } = App.useApp()
  const [parts, setParts] = useState<number | null>(null)
  const [editKey, setEditKey] = useState('')
  const [editVal, setEditVal] = useState('')
  if (!detail) return null
  return (
    <Drawer title={detail.name} width={820} open onClose={onClose}>
      <Descriptions size="small" bordered column={3}>
        <Descriptions.Item label="分区">{detail.partitions}</Descriptions.Item>
        <Descriptions.Item label="副本">{detail.replication}</Descriptions.Item>
        <Descriptions.Item label="消息数（估算）">{detail.messages.toLocaleString()}</Descriptions.Item>
      </Descriptions>
      <Tabs
        items={[
          {
            key: 'p', label: '分区',
            children: (
              <>
                {!inst.readOnly && (
                  <Space style={{ marginBottom: 8 }}>
                    <span>扩容到</span>
                    <InputNumber min={detail.partitions + 1} value={parts ?? undefined} onChange={(n) => setParts(n)} />
                    <span>个分区</span>
                    <Button disabled={!parts} onClick={async () => {
                      try {
                        await api.kafkaSetPartitions(inst.id, detail.name, parts!)
                        message.success('已扩容')
                        onChanged(detail.name)
                      } catch (e) { message.error(errMsg(e)) }
                    }}>执行</Button>
                    <Typography.Text type="secondary">分区只能增加；有 key 的消息路由会改变</Typography.Text>
                  </Space>
                )}
                <Table size="small" rowKey="id" pagination={false} dataSource={detail.partitionList}
                  columns={[
                    { title: '分区', dataIndex: 'id', width: 70 },
                    { title: 'Leader', dataIndex: 'leader', width: 80 },
                    { title: '副本', dataIndex: 'replicas', render: (r: number[]) => r?.join(', ') },
                    {
                      title: 'ISR', dataIndex: 'isr',
                      render: (isr: number[], p) => <span style={{ color: (isr?.length ?? 0) < (p.replicas?.length ?? 0) ? '#e03131' : undefined }}>{isr?.join(', ')}</span>,
                    },
                    { title: '起始 offset', dataIndex: 'startOffset' },
                    { title: '结束 offset', dataIndex: 'endOffset' },
                    { title: '消息数', render: (_, p) => (p.endOffset - p.startOffset).toLocaleString() },
                  ]} />
              </>
            ),
          },
          {
            key: 'c', label: '配置',
            children: (
              <>
                {!inst.readOnly && (
                  <Space style={{ marginBottom: 8 }}>
                    <Input placeholder="配置项" value={editKey} onChange={(e) => setEditKey(e.target.value)} style={{ width: 220 }} list="kafka-cfg-keys2" />
                    <Input placeholder="值（留空表示恢复默认）" value={editVal} onChange={(e) => setEditVal(e.target.value)} style={{ width: 220 }} />
                    <Button disabled={!editKey} onClick={async () => {
                      try {
                        await api.kafkaAlterTopicConfigs(inst.id, detail.name, { [editKey]: editVal })
                        message.success('已修改')
                        onChanged(detail.name)
                      } catch (e) { message.error(errMsg(e)) }
                    }}>修改</Button>
                    <datalist id="kafka-cfg-keys2">{commonConfigs.map((c) => <option key={c} value={c} />)}</datalist>
                  </Space>
                )}
                <Table size="small" rowKey="key" pagination={false} dataSource={detail.configs ?? []}
                  onRow={(r) => ({ onClick: () => { setEditKey(r.key); setEditVal(r.value) } })}
                  columns={[
                    { title: 'Key', dataIndex: 'key', render: (k, r) => <span style={{ fontWeight: r.source.includes('DYNAMIC_TOPIC') ? 600 : 400 }}>{k}</span> },
                    { title: 'Value', dataIndex: 'value', render: (v, r) => (r.sensitive ? '******' : <span className="mono">{v}</span>) },
                    { title: '来源', dataIndex: 'source', render: (s) => <Tag>{s}</Tag> },
                  ]} />
              </>
            ),
          },
        ]}
      />
    </Drawer>
  )
}
