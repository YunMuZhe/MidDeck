import { useEffect, useState } from 'react'
import { App, Button, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { ExportOutlined, SendOutlined, MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { api, errMsg } from '../api'
import { fmtTime, prettyJSON } from '../common'
import type { Instance, Message, Topic } from '../types'

export default function MessagesTab({ inst, topic }: { inst: Instance; topic?: string }) {
  const { message } = App.useApp()
  const [form] = Form.useForm()
  const [topics, setTopics] = useState<Topic[]>([])
  const [msgs, setMsgs] = useState<Message[]>([])
  const [loading, setLoading] = useState(false)
  const [produceOpen, setProduceOpen] = useState(false)
  const mode = Form.useWatch('mode', form)
  const curTopic = Form.useWatch('topic', form)
  const partitions = topics.find((t) => t.name === curTopic)?.partitions ?? 0

  useEffect(() => {
    api.kafkaTopics(inst.id).then(setTopics).catch(() => {})
  }, [inst.id])

  useEffect(() => {
    if (topic) {
      form.setFieldValue('topic', topic)
      search()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [topic])

  const search = async () => {
    const v = await form.validateFields()
    setLoading(true)
    try {
      const r = await api.kafkaFetch(inst.id, {
        topic: v.topic, partition: v.partition ?? -1, mode: v.mode, offset: v.offset ?? 0,
        timestamp: v.time ? v.time.valueOf() : 0, limit: v.limit ?? 100, keyFilter: v.keyFilter ?? '', contains: v.contains ?? '',
      })
      setMsgs(r)
      if (r.length === 0) message.info('没有符合条件的消息')
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <>
      <Form form={form} layout="inline" initialValues={{ mode: 'newest', partition: -1, limit: 100 }} style={{ rowGap: 8, marginBottom: 12 }}>
        <Form.Item name="topic" rules={[{ required: true, message: '选择 topic' }]}>
          <Select showSearch placeholder="Topic" style={{ width: 260 }} options={topics.filter((t) => !t.internal).map((t) => ({ value: t.name }))} />
        </Form.Item>
        <Form.Item name="partition">
          <Select style={{ width: 110 }} options={[{ value: -1, label: '全部分区' }, ...Array.from({ length: partitions }, (_, i) => ({ value: i, label: `分区 ${i}` }))]} />
        </Form.Item>
        <Form.Item name="mode">
          <Select style={{ width: 130 }} options={[
            { value: 'newest', label: '最新 N 条' }, { value: 'oldest', label: '最早 N 条' },
            { value: 'offset', label: '从 offset' }, { value: 'timestamp', label: '从时间点' },
          ]} />
        </Form.Item>
        {mode === 'offset' && <Form.Item name="offset"><InputNumber placeholder="offset" min={0} /></Form.Item>}
        {mode === 'timestamp' && <Form.Item name="time" initialValue={dayjs().subtract(1, 'hour')}><DatePicker showTime /></Form.Item>}
        <Form.Item name="limit"><InputNumber min={1} max={5000} addonBefore="条数" style={{ width: 150 }} /></Form.Item>
        <Form.Item name="keyFilter"><Input placeholder="Key 精确匹配" allowClear style={{ width: 160 }} /></Form.Item>
        <Form.Item name="contains"><Input placeholder="Value 包含" allowClear style={{ width: 180 }} /></Form.Item>
        <Space>
          <Button type="primary" onClick={search} loading={loading}>查询</Button>
          <Button icon={<SendOutlined />} onClick={() => setProduceOpen(true)} disabled={inst.readOnly}>发送消息</Button>
          <Button icon={<ExportOutlined />} disabled={msgs.length === 0} onClick={async () => {
            try {
              const p = await api.exportMessages(curTopic ?? 'messages', msgs)
              if (p) message.success('已导出到 ' + p)
            } catch (e) { message.error(errMsg(e)) }
          }}>导出 JSONL</Button>
        </Space>
      </Form>
      <Table
        size="small"
        rowKey={(m) => `${m.partition}-${m.offset}`}
        loading={loading}
        dataSource={msgs}
        pagination={{ pageSize: 50 }}
        expandable={{
          expandedRowRender: (m) => (
            <Space direction="vertical" style={{ width: '100%' }}>
              {m.headers && m.headers.length > 0 && (
                <div>{m.headers.map((h) => <Tag key={h.key}>{h.key}: {h.value}</Tag>)}</div>
              )}
              <Typography.Paragraph copyable={{ text: m.value }} style={{ marginBottom: 0 }}>
                <pre className="mono">{prettyJSON(m.value)}</pre>
              </Typography.Paragraph>
            </Space>
          ),
        }}
        columns={[
          { title: '分区', dataIndex: 'partition', width: 60 },
          { title: 'Offset', dataIndex: 'offset', width: 100 },
          { title: '时间', dataIndex: 'timestamp', width: 170, render: fmtTime },
          { title: 'Key', dataIndex: 'key', width: 180, render: (k) => <span className="mono cell-ellipsis" style={{ maxWidth: 170 }}>{k}</span> },
          { title: 'Value', dataIndex: 'value', render: (v) => <span className="mono cell-ellipsis">{v}</span> },
          { title: '大小', dataIndex: 'size', width: 80, render: (s) => `${s} B` },
        ]}
      />
      <ProduceModal inst={inst} topics={topics} defaultTopic={curTopic} open={produceOpen} onClose={() => setProduceOpen(false)} />
    </>
  )
}

function ProduceModal({ inst, topics, defaultTopic, open, onClose }: { inst: Instance; topics: Topic[]; defaultTopic?: string; open: boolean; onClose: () => void }) {
  const { message } = App.useApp()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (open) form.setFieldsValue({ topic: defaultTopic, partition: -1 })
  }, [open, defaultTopic, form])
  const submit = async () => {
    const v = await form.validateFields()
    setBusy(true)
    try {
      const m = await api.kafkaProduce(inst.id, { topic: v.topic, partition: v.partition ?? -1, key: v.key ?? '', value: v.value ?? '', headers: (v.headers ?? []).filter((h: { key?: string }) => h?.key) })
      message.success(`已发送到分区 ${m.partition}，offset ${m.offset}`)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  const parts = topics.find((t) => t.name === Form.useWatch('topic', form))?.partitions ?? 0
  return (
    <Modal title="发送消息" open={open} onCancel={onClose} onOk={submit} okText="发送" confirmLoading={busy} width={680}>
      <Form form={form} layout="vertical">
        <Space style={{ width: '100%' }} align="start">
          <Form.Item name="topic" label="Topic" rules={[{ required: true }]}>
            <Select showSearch style={{ width: 300 }} options={topics.filter((t) => !t.internal).map((t) => ({ value: t.name }))} />
          </Form.Item>
          <Form.Item name="partition" label="分区">
            <Select style={{ width: 140 }} options={[{ value: -1, label: '按 key 自动' }, ...Array.from({ length: parts }, (_, i) => ({ value: i, label: `分区 ${i}` }))]} />
          </Form.Item>
        </Space>
        <Form.Item name="key" label="Key"><Input /></Form.Item>
        <Form.Item name="value" label="Value"><Input.TextArea rows={8} className="mono" placeholder='{"orderId": 1}' /></Form.Item>
        <Typography.Text>Headers</Typography.Text>
        <Form.List name="headers">
          {(fields, { add, remove }) => (
            <>
              {fields.map((f) => (
                <Space key={f.key} align="baseline" style={{ display: 'flex' }}>
                  <Form.Item name={[f.name, 'key']}><Input placeholder="key" /></Form.Item>
                  <Form.Item name={[f.name, 'value']}><Input placeholder="value" style={{ width: 320 }} /></Form.Item>
                  <MinusCircleOutlined onClick={() => remove(f.name)} />
                </Space>
              ))}
              <Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => add()} style={{ marginTop: 8 }}>添加 Header</Button>
            </>
          )}
        </Form.List>
      </Form>
    </Modal>
  )
}
