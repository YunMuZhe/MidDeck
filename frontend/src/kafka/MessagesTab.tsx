import { useEffect, useState } from 'react'
import { App, Button, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { ExportOutlined, MinusCircleOutlined, PlusOutlined, SearchOutlined, SendOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { api, errMsg } from '../api'
import { fmtBytes, fmtTime, prettyJSON } from '../common'
import { useT } from '../settings'
import type { Instance, Message, Topic } from '../types'

export default function MessagesTab({ inst, topic }: { inst: Instance; topic?: { name: string; n: number } }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  const [topics, setTopics] = useState<Topic[]>([])
  const [msgs, setMsgs] = useState<Message[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [produceOpen, setProduceOpen] = useState(false)
  const mode = Form.useWatch('mode', form)
  const curTopic = Form.useWatch('topic', form)
  const partitions = topics.find((x) => x.name === curTopic)?.partitions ?? 0

  useEffect(() => {
    api.kafkaTopics(inst.id).then(setTopics).catch(() => {})
  }, [inst.id])

  const search = async () => {
    const v = await form.validateFields()
    setLoading(true)
    try {
      const r = await api.kafkaFetch(inst.id, {
        topic: v.topic, partition: v.partition ?? -1, mode: v.mode, offset: v.offset ?? 0,
        timestamp: v.time ? v.time.valueOf() : 0, limit: v.limit ?? 100, keyFilter: v.keyFilter ?? '', contains: v.contains ?? '',
      })
      setMsgs(r)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (topic) {
      form.setFieldsValue({ topic: topic.name, partition: -1 })
      search()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [topic])

  return (
    <>
      <Form form={form} initialValues={{ mode: 'newest', partition: -1, limit: 100 }}>
        <div className="toolbar">
          <Form.Item name="topic" noStyle rules={[{ required: true, message: t('Choose a topic') }]}>
            <Select showSearch placeholder={t('Topic')} style={{ width: 240 }} options={topics.filter((x) => !x.internal).map((x) => ({ value: x.name }))} />
          </Form.Item>
          <Form.Item name="partition" noStyle>
            <Select style={{ width: 130 }} options={[{ value: -1, label: t('All partitions') }, ...Array.from({ length: partitions }, (_, i) => ({ value: i, label: t('Partition {n}', { n: i }) }))]} />
          </Form.Item>
          <Form.Item name="mode" noStyle>
            <Select style={{ width: 150 }} options={[
              { value: 'newest', label: t('Newest N') }, { value: 'oldest', label: t('Oldest N') },
              { value: 'offset', label: t('From offset') }, { value: 'timestamp', label: t('From time') },
            ]} />
          </Form.Item>
          {mode === 'offset' && <Form.Item name="offset" noStyle><InputNumber placeholder="offset" min={0} /></Form.Item>}
          {mode === 'timestamp' && <Form.Item name="time" noStyle initialValue={dayjs().subtract(1, 'hour')}><DatePicker showTime /></Form.Item>}
          <Form.Item name="limit" noStyle><InputNumber min={1} max={5000} addonBefore={t('Limit')} style={{ width: 150 }} /></Form.Item>
        </div>
        <div className="toolbar">
          <Form.Item name="keyFilter" noStyle><Input placeholder={t('Key equals')} allowClear style={{ width: 200 }} /></Form.Item>
          <Form.Item name="contains" noStyle><Input placeholder={t('Value contains')} allowClear style={{ width: 240 }} /></Form.Item>
          <Button type="primary" icon={<SearchOutlined />} onClick={search} loading={loading}>{t('Search')}</Button>
          <div className="spacer" />
          <Button icon={<ExportOutlined />} disabled={!msgs?.length} onClick={async () => {
            try {
              const p = await api.exportMessages(curTopic ?? 'messages', msgs ?? [])
              if (p) message.success(t('Exported to {path}', { path: p }))
            } catch (e) { message.error(errMsg(e)) }
          }}>{t('Export JSONL')}</Button>
          <Button icon={<SendOutlined />} onClick={() => setProduceOpen(true)} disabled={inst.readOnly}>{t('Produce')}</Button>
        </div>
      </Form>
      <Table
        size="middle"
        rowKey={(m) => `${m.partition}-${m.offset}`}
        loading={loading}
        dataSource={msgs ?? []}
        locale={{ emptyText: msgs === null ? t('Choose a topic and search') : t('No messages match') }}
        pagination={{ pageSize: 50, hideOnSinglePage: true }}
        expandable={{
          expandRowByClick: true,
          expandedRowRender: (m) => (
            <Space direction="vertical" style={{ width: '100%' }}>
              {!!m.headers?.length && <div>{m.headers.map((h) => <Tag key={h.key} bordered={false}>{h.key}: {h.value}</Tag>)}</div>}
              <div className="code-block">
                <Typography.Paragraph copyable={{ text: m.value }} style={{ marginBottom: 0 }}>
                  <pre className="mono">{prettyJSON(m.value)}</pre>
                </Typography.Paragraph>
              </div>
            </Space>
          ),
        }}
        columns={[
          { title: t('Partition'), dataIndex: 'partition', width: 90 },
          { title: 'Offset', dataIndex: 'offset', width: 100 },
          { title: t('Time'), dataIndex: 'timestamp', width: 170, render: (x) => <span style={{ whiteSpace: 'nowrap' }}>{fmtTime(x)}</span> },
          { title: 'Key', dataIndex: 'key', width: 170, render: (k) => <span className="mono cell-ellipsis" style={{ maxWidth: 160 }}>{k}</span> },
          { title: 'Value', dataIndex: 'value', render: (v) => <span className="mono cell-ellipsis">{v}</span> },
          { title: t('Size'), dataIndex: 'size', width: 90, align: 'right', render: fmtBytes },
        ]}
      />
      <ProduceModal inst={inst} topics={topics} defaultTopic={curTopic} open={produceOpen} onClose={() => setProduceOpen(false)} />
    </>
  )
}

function ProduceModal({ inst, topics, defaultTopic, open, onClose }: { inst: Instance; topics: Topic[]; defaultTopic?: string; open: boolean; onClose: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const cur = Form.useWatch('topic', form)
  useEffect(() => {
    if (open) form.setFieldsValue({ topic: defaultTopic, partition: -1 })
  }, [open, defaultTopic, form])
  const submit = async () => {
    const v = await form.validateFields()
    setBusy(true)
    try {
      const m = await api.kafkaProduce(inst.id, {
        topic: v.topic, partition: v.partition ?? -1, key: v.key ?? '', value: v.value ?? '',
        headers: (v.headers ?? []).filter((h: { key?: string }) => h?.key),
      })
      message.success(t('Sent to partition {p}, offset {o}', { p: m.partition, o: m.offset }))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  const parts = topics.find((x) => x.name === cur)?.partitions ?? 0
  return (
    <Modal title={t('Produce message')} open={open} onCancel={onClose} onOk={submit} okText={t('Send')} cancelText={t('Close')} confirmLoading={busy} width={680}>
      <Form form={form} layout="vertical" requiredMark={false}>
        <Space style={{ width: '100%' }} align="start">
          <Form.Item name="topic" label={t('Topic')} rules={[{ required: true, message: t('Required') }]}>
            <Select showSearch style={{ width: 320 }} options={topics.filter((x) => !x.internal).map((x) => ({ value: x.name }))} />
          </Form.Item>
          <Form.Item name="partition" label={t('Partition')}>
            <Select style={{ width: 160 }} options={[{ value: -1, label: t('By key') }, ...Array.from({ length: parts }, (_, i) => ({ value: i, label: t('Partition {n}', { n: i }) }))]} />
          </Form.Item>
        </Space>
        <Form.Item name="key" label="Key"><Input /></Form.Item>
        <Form.Item name="value" label="Value"><Input.TextArea rows={8} className="mono" placeholder='{"orderId": 1}' /></Form.Item>
        <div className="muted" style={{ marginBottom: 6 }}>Headers</div>
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
              <Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => add()}>{t('Add header')}</Button>
            </>
          )}
        </Form.List>
      </Form>
    </Modal>
  )
}
