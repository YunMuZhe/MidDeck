import { useEffect, useState } from 'react'
import { Alert, App, Button, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Tooltip, Typography } from 'antd'
import { CodeOutlined, ExportOutlined, MinusCircleOutlined, PlusOutlined, ReloadOutlined, SearchOutlined, SendOutlined, WarningOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { api, errMsg } from '../api'
import { fmtBytes, fmtTime, prettyJSON } from '../common'
import { useT } from '../settings'
import type { ActionArgs, Instance, Message, ProtoTypes, Topic } from '../types'

export default function MessagesTab({ inst, topic, prefill }: { inst: Instance; topic?: { name: string; n: number }; prefill?: ActionArgs }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  const [topics, setTopics] = useState<Topic[]>([])
  const [msgs, setMsgs] = useState<Message[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [produceOpen, setProduceOpen] = useState(false)
  const [decodeOpen, setDecodeOpen] = useState(false)
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
        decode: v.decode === 'raw' ? 'raw' : '',
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

  useEffect(() => {
    if (!prefill) return
    form.resetFields()
    form.setFieldsValue({
      topic: prefill.topic,
      partition: prefill.partition ?? -1,
      mode: prefill.mode ?? (prefill.since ? 'timestamp' : 'newest'),
      offset: prefill.offset,
      time: prefill.since ? dayjs(prefill.since) : undefined,
      limit: prefill.limit ?? 100,
      keyFilter: prefill.key,
      contains: prefill.contains,
    })
  }, [prefill, form])

  return (
    <>
      <Form form={form} initialValues={{ mode: 'newest', partition: -1, limit: 100, decode: '' }}>
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
          <Form.Item name="decode" noStyle>
            <Select style={{ width: 140 }} options={[{ value: '', label: t('Decoded') }, { value: 'raw', label: t('Raw bytes') }]} />
          </Form.Item>
          <Button type="primary" icon={<SearchOutlined />} onClick={search} loading={loading}>{t('Search')}</Button>
          <div className="spacer" />
          <Button icon={<CodeOutlined />} disabled={!curTopic} onClick={() => setDecodeOpen(true)}>{t('Decoding')}</Button>
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
              {m.decodeError && <Alert type="warning" showIcon message={t('Shown undecoded')} description={m.decodeError} />}
              {(m.valueSchema || m.keySchema) && (
                <div className="muted">
                  {m.keySchema && <span style={{ marginRight: 16 }}>Key · {m.keySchema}</span>}
                  {m.valueSchema && <span>Value · {m.valueSchema}</span>}
                </div>
              )}
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
          { title: 'Key', dataIndex: 'key', width: 170, render: (k, m) => <span className="cell-flex"><FormatTag f={m.keyFormat} /><span className="mono cell-ellipsis" style={{ maxWidth: 160 }}>{k}</span></span> },
          {
            title: 'Value', dataIndex: 'value', render: (v, m) => (
              <span className="cell-flex">
                {m.decodeError && <Tooltip title={m.decodeError}><WarningOutlined style={{ color: '#f08c00' }} /></Tooltip>}
                <FormatTag f={m.valueFormat} schema={m.valueSchema} />
                <span className="mono cell-ellipsis">{v}</span>
              </span>
            ),
          },
          { title: t('Size'), dataIndex: 'size', width: 90, align: 'right', render: fmtBytes },
        ]}
      />
      {decodeOpen && curTopic && <DecodeModal inst={inst} topic={curTopic} onClose={() => setDecodeOpen(false)} onSaved={() => { setDecodeOpen(false); if (msgs) search() }} />}
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

const formatLabel: Record<string, string> = { avro: 'Avro', protobuf: 'Protobuf', binary: 'base64' }

/** Small tag saying how a key/value was decoded; plain text and JSON get none. */
export function FormatTag({ f, schema }: { f?: string; schema?: string }) {
  if (!f || !formatLabel[f]) return null
  const tag = <Tag bordered={false} color={f === 'binary' ? 'default' : 'blue'} className="fmt-tag">{formatLabel[f]}</Tag>
  return schema ? <Tooltip title={schema}>{tag}</Tooltip> : tag
}

/** Binds local Protobuf types to a topic (or topic pattern) for decoding. */
function DecodeModal({ inst, topic, onClose, onSaved }: { inst: Instance; topic: string; onClose: () => void; onSaved: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  const [info, setInfo] = useState<ProtoTypes | null>(null)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)

  const load = async (reload: boolean) => {
    setLoading(true)
    try {
      const r = await api.kafkaProtoTypes(inst.id, reload)
      setInfo(r)
      if (!reload) {
        const b = r.bindings.find((x) => x.topic === topic) ?? r.bindings.find((x) => x.topic.includes('*') && globMatch(x.topic, topic))
        form.setFieldsValue({ pattern: b?.topic ?? topic, key: b?.key || undefined, value: b?.value || undefined })
      }
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    load(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inst.id, topic])

  const save = async (clear = false) => {
    const v = await form.validateFields()
    setBusy(true)
    try {
      await api.kafkaSetProtoBinding(inst.id, { topic: v.pattern.trim(), key: clear ? '' : v.key ?? '', value: clear ? '' : v.value ?? '' })
      message.success(t('Saved'))
      onSaved()
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  const opts = (info?.types ?? []).map((x) => ({ value: x }))
  const noFiles = info && !(info.paths ?? []).length
  return (
    <Modal
      open
      title={t('Decoding · {topic}', { topic })}
      onCancel={onClose}
      width={620}
      footer={[
        <Button key="clear" danger onClick={() => save(true)} disabled={busy}>{t('Remove binding')}</Button>,
        <Button key="cancel" onClick={onClose}>{t('Cancel')}</Button>,
        <Button key="ok" type="primary" loading={busy} onClick={() => save()} disabled={noFiles ?? true}>{t('Save')}</Button>,
      ]}
    >
      <Typography.Paragraph type="secondary">
        {info?.registry
          ? t('Messages in the Schema Registry format are decoded automatically with {url}.', { url: info.registry })
          : t('No Schema Registry is configured; registry-encoded messages stay raw. Add one in the connection settings under Decoding.')}
      </Typography.Paragraph>
      <Typography.Paragraph style={{ marginBottom: 12 }}>
        {t('For plain Protobuf (no registry), choose the message types of this topic:')}
      </Typography.Paragraph>
      {noFiles && <Alert type="info" showIcon style={{ marginBottom: 12 }} message={t('No .proto files configured. Add files or directories in the connection settings under Decoding.')} />}
      {!!info?.problems.length && (
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={t('Some .proto files did not compile')}
          description={<div className="mono" style={{ maxHeight: 120, overflow: 'auto', whiteSpace: 'pre-wrap', fontSize: 12 }}>{info.problems.join('\n')}</div>} />
      )}
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item name="pattern" label={t('Topic or pattern')} extra={t('Use * for families of topics, e.g. orders.*')} rules={[{ required: true, message: t('Required') }]}>
          <Input />
        </Form.Item>
        <Form.Item name="value" label={t('Value type')}>
          <Select showSearch allowClear loading={loading} options={opts} placeholder="acme.orders.v1.Order" disabled={!!noFiles} />
        </Form.Item>
        <Form.Item name="key" label={t('Key type (usually empty: keys are often plain strings)')}>
          <Select showSearch allowClear loading={loading} options={opts} disabled={!!noFiles} />
        </Form.Item>
      </Form>
      <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={() => load(true)} disabled={!!noFiles}>
        {t('Reload .proto files ({n} types)', { n: info?.types.length ?? 0 })}
      </Button>
    </Modal>
  )
}

function globMatch(pattern: string, s: string) {
  const re = new RegExp('^' + pattern.replace(/[.+^${}()|\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.') + '$')
  return re.test(s)
}
