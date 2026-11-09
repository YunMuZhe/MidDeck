import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, App, Badge, Button, Checkbox, Drawer, Form, Input, InputNumber, Modal, Select, Space, Table, Tabs, Tag, Typography } from 'antd'
import { ExclamationCircleFilled, ExportOutlined, PlusOutlined, ReloadOutlined, SearchOutlined, SendOutlined } from '@ant-design/icons'
import { api, errMsg } from '../api'
import { CopyCommand, fmtBytes, num, prettyJSON, Tiles, useDangerConfirm } from '../common'
import { useT } from '../settings'
import type { Binding, Exchange, Instance, PeekedMessage, Queue } from '../types'

export default function RabbitView({ inst }: { inst: Instance }) {
  const { message } = App.useApp()
  const t = useT()
  const [queues, setQueues] = useState<Queue[] | null>(null)
  const [exchanges, setExchanges] = useState<Exchange[]>([])
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [peek, setPeek] = useState<Queue | null>(null)
  const [createQ, setCreateQ] = useState(false)
  const [createX, setCreateX] = useState(false)
  const [publish, setPublish] = useState<{ exchange: string; routingKey: string } | null>(null)
  const confirm = useDangerConfirm(inst)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [qs, xs] = await Promise.all([api.rabbitQueues(inst.id), api.rabbitExchanges(inst.id)])
      setQueues(qs)
      setExchanges(xs)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [inst.id, message])

  useEffect(() => {
    load()
  }, [load])

  const qRows = useMemo(() => (queues ?? []).filter((x) => x.name.toLowerCase().includes(q.toLowerCase())), [queues, q])
  const xRows = useMemo(() => exchanges.filter((x) => x.name.toLowerCase().includes(q.toLowerCase())), [exchanges, q])
  const danger = (title: string, name: string, fn: () => Promise<void>) =>
    confirm(title, name, async () => {
      try {
        await fn()
        message.success(t('Done'))
        load()
      } catch (e) {
        message.error(errMsg(e))
        throw e
      }
    })

  const sum = (f: (x: Queue) => number) => (queues ? queues.reduce((s, x) => s + f(x), 0) : undefined)
  const idle = queues?.filter((x) => x.consumers === 0 && x.messages_ready > 0).length ?? 0

  const toolbar = (
    <div className="toolbar">
      <Input prefix={<SearchOutlined className="muted" />} placeholder={t('Filter by name')} allowClear value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 260 }} />
      <div className="spacer" />
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading} />
      <Button icon={<ExportOutlined />} onClick={async () => {
        try { const p = await api.rabbitExportDefinitions(inst.id); if (p) message.success(t('Exported to {path}', { path: p })) } catch (e) { message.error(errMsg(e)) }
      }}>{t('Export definitions')}</Button>
      <Button icon={<SendOutlined />} disabled={inst.readOnly} onClick={() => setPublish({ exchange: '', routingKey: '' })}>{t('Publish')}</Button>
      <Button icon={<PlusOutlined />} disabled={inst.readOnly} onClick={() => setCreateX(true)}>{t('New exchange')}</Button>
      <Button icon={<PlusOutlined />} type="primary" disabled={inst.readOnly} onClick={() => setCreateQ(true)}>{t('New queue')}</Button>
    </div>
  )

  return (
    <>
      <Tiles
        items={[
          { label: t('Queues'), value: num(queues?.length), note: t('{n} exchanges', { n: exchanges.length }) },
          { label: t('Ready'), value: num(sum((x) => x.messages_ready)) },
          { label: t('Unacked'), value: num(sum((x) => x.messages_unacknowledged)) },
          {
            label: t('Consumers'),
            value: num(sum((x) => x.consumers)),
            note: idle > 0 ? <><ExclamationCircleFilled style={{ color: '#f08c00' }} /> {t('{n} queue(s) with messages but no consumer', { n: idle })}</> : undefined,
          },
        ]}
      />
      <Tabs
        items={[
          {
            key: 'q', label: t('Queues'),
            children: (
              <>
                {toolbar}
                <Table size="middle" rowKey="name" loading={loading} dataSource={qRows} pagination={{ pageSize: 50, hideOnSinglePage: true }}
                  onRow={(r) => ({ onClick: () => setPeek(r), style: { cursor: 'pointer' } })}
                  columns={[
                    { title: t('Queue'), dataIndex: 'name', render: (n) => <a>{n}</a>, sorter: (a, b) => a.name.localeCompare(b.name) },
                    { title: t('Type'), dataIndex: 'type', width: 100, render: (x) => <Tag bordered={false}>{x}</Tag> },
                    { title: t('State'), dataIndex: 'state', width: 110, render: (s) => <Badge status={s === 'running' ? 'success' : 'warning'} text={s} /> },
                    { title: t('Ready'), dataIndex: 'messages_ready', width: 100, align: 'right', sorter: (a, b) => a.messages_ready - b.messages_ready, render: (n) => <b>{num(n)}</b> },
                    { title: t('Unacked'), dataIndex: 'messages_unacknowledged', width: 100, align: 'right', render: num },
                    {
                      title: t('Consumers'), dataIndex: 'consumers', width: 110, align: 'right',
                      render: (c, r) => (c === 0 && r.messages_ready > 0 ? <span><Badge status="warning" /> 0</span> : c),
                    },
                    {
                      title: '', width: 140, align: 'right',
                      render: (_, r) => !inst.readOnly && (
                        <Space size="middle" onClick={(e) => e.stopPropagation()}>
                          <a onClick={() => danger(t('Purge queue (messages cannot be recovered)'), r.name, () => api.rabbitPurgeQueue(inst.id, r.name))}>{t('Purge')}</a>
                          <Typography.Link type="danger" onClick={() => danger(t('Delete queue'), r.name, () => api.rabbitDeleteQueue(inst.id, r.name))}>{t('Delete')}</Typography.Link>
                        </Space>
                      ),
                    },
                  ]} />
              </>
            ),
          },
          {
            key: 'x', label: t('Exchanges'),
            children: (
              <>
                {toolbar}
                <Table size="middle" rowKey="name" loading={loading} dataSource={xRows} pagination={{ pageSize: 50, hideOnSinglePage: true }}
                  columns={[
                    { title: t('Exchange'), dataIndex: 'name', render: (n) => n || <span className="muted">(AMQP default)</span> },
                    { title: t('Type'), dataIndex: 'type', width: 110, render: (x) => <Tag bordered={false}>{x}</Tag> },
                    { title: t('Durable'), dataIndex: 'durable', width: 100, render: (d) => (d ? '✓' : '') },
                    { title: t('Internal'), dataIndex: 'internal', width: 100, render: (d) => (d ? '✓' : '') },
                    { title: '', width: 120, align: 'right', render: (_, r) => !inst.readOnly && !r.internal && <a onClick={() => setPublish({ exchange: r.name, routingKey: '' })}>{t('Publish')}</a> },
                  ]} />
              </>
            ),
          },
        ]}
      />
      {peek && <PeekDrawer inst={inst} queue={peek} onClose={() => setPeek(null)} />}
      <CreateQueueModal inst={inst} open={createQ} exchanges={exchanges} onClose={() => setCreateQ(false)} onDone={load} />
      <CreateExchangeModal inst={inst} open={createX} onClose={() => setCreateX(false)} onDone={load} />
      {publish && <PublishModal inst={inst} init={publish} exchanges={exchanges} onClose={() => { setPublish(null); load() }} />}
    </>
  )
}

function PeekDrawer({ inst, queue, onClose }: { inst: Instance; queue: Queue; onClose: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [n, setN] = useState(20)
  const [msgs, setMsgs] = useState<PeekedMessage[] | null>(null)
  const [bindings, setBindings] = useState<Binding[]>([])
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    api.rabbitQueueBindings(inst.id, queue.name).then(setBindings).catch(() => {})
  }, [inst.id, queue.name])
  const load = async () => {
    setLoading(true)
    try {
      setMsgs(await api.rabbitPeek(inst.id, queue.name, n))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }
  return (
    <Drawer title={queue.name} width={900} open onClose={onClose}>
      <Space direction="vertical" style={{ width: '100%' }} size={14}>
        <Space wrap>
          <Tag bordered={false}>{queue.type}</Tag>
          <span>{t('Ready')} <b>{num(queue.messages_ready)}</b></span>
          <span className="muted">·</span>
          <span>{t('Unacked')} {num(queue.messages_unacknowledged)}</span>
          <span className="muted">·</span>
          <span>{t('Consumers')} {queue.consumers}</span>
          {queue.arguments && Object.entries(queue.arguments).map(([k, v]) => <Tag key={k} bordered={false} className="mono">{k}={String(v)}</Tag>)}
        </Space>
        <div>
          <div className="section-title">{t('Bindings')}</div>
          <Table size="small" rowKey={(b) => b.source + b.routing_key} pagination={false} dataSource={bindings}
            columns={[{ title: t('Exchange'), dataIndex: 'source', render: (s) => s || '(default)' }, { title: 'Routing key', dataIndex: 'routing_key', render: (x) => <span className="mono">{x}</span> }]} />
        </div>
        <div>
          <div className="section-title">{t('Messages')}</div>
          <Alert type="info" showIcon style={{ marginBottom: 10 }} message={t('Peeking fetches messages and requeues them immediately: they get marked redelivered and may change position in classic queues.')} />
          <div className="toolbar">
            <InputNumber min={1} max={500} value={n} onChange={(v) => setN(v ?? 20)} addonBefore={t('Count')} style={{ width: 160 }} />
            <Button type="primary" onClick={load} loading={loading}>{t('Peek messages')}</Button>
          </div>
          <Table size="small" rowKey={(_, i) => String(i)} dataSource={msgs ?? []} pagination={false}
            locale={{ emptyText: msgs === null ? t('Click "Peek messages" to load') : t('Queue is empty') }}
            expandable={{
              expandRowByClick: true,
              expandedRowRender: (m) => <div className="code-block"><pre className="mono">{prettyJSON(m.payload)}{'\n\nproperties: ' + JSON.stringify(m.properties, null, 2)}</pre></div>,
            }}
            columns={[
              { title: t('Exchange'), dataIndex: 'exchange', width: 150 },
              { title: 'Routing key', dataIndex: 'routing_key', width: 170, render: (x) => <span className="mono">{x}</span> },
              { title: 'Payload', dataIndex: 'payload', render: (p) => <span className="mono cell-ellipsis" style={{ maxWidth: 360 }}>{p}</span> },
              { title: t('Size'), dataIndex: 'payload_bytes', width: 80, align: 'right', render: fmtBytes },
              { title: t('Redelivered'), dataIndex: 'redelivered', width: 110, render: (r) => (r ? '✓' : '') },
            ]} />
        </div>
      </Space>
    </Drawer>
  )
}

function CreateQueueModal({ inst, open, exchanges, onClose, onDone }: { inst: Instance; open: boolean; exchanges: Exchange[]; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  const v = Form.useWatch([], form) ?? {}
  const submit = async () => {
    const x = await form.validateFields()
    const args: Record<string, unknown> = {}
    if (x.dlx) args['x-dead-letter-exchange'] = x.dlx
    if (x.ttl) args['x-message-ttl'] = x.ttl
    if (x.maxLength) args['x-max-length'] = x.maxLength
    try {
      await api.rabbitCreateQueue(inst.id, { name: x.name, type: x.type, durable: x.durable, arguments: args, bindExchange: x.bindExchange ?? '', routingKey: x.routingKey ?? '' })
      message.success(t('Created {name}', { name: x.name }))
      form.resetFields()
      onClose()
      onDone()
    } catch (e) {
      message.error(errMsg(e))
    }
  }
  const vhost = inst.vhost || '/'
  const cmd = `rabbitmqadmin -V ${vhost} declare queue name=${v.name || '<queue>'} durable=${v.durable ?? true}` +
    (v.type && v.type !== 'classic' ? ` queue_type=${v.type}` : '') +
    (v.bindExchange ? `\nrabbitmqadmin -V ${vhost} declare binding source=${v.bindExchange} destination=${v.name || '<queue>'} routing_key=${v.routingKey ?? ''}` : '')
  return (
    <Modal title={t('New queue')} open={open} onCancel={onClose} onOk={submit} okText={t('Create')} cancelText={t('Cancel')} width={620} destroyOnClose>
      <Form form={form} layout="vertical" requiredMark={false} initialValues={{ type: 'quorum', durable: true }}>
        <Form.Item name="name" label={t('Name')} rules={[{ required: true, message: t('Required') }]}><Input /></Form.Item>
        <Space size="large">
          <Form.Item name="type" label={t('Type')}><Select style={{ width: 160 }} options={[{ value: 'classic' }, { value: 'quorum' }, { value: 'stream' }]} /></Form.Item>
          <Form.Item name="durable" label=" " valuePropName="checked"><Checkbox>{t('Durable')}</Checkbox></Form.Item>
        </Space>
        <Space>
          <Form.Item name="dlx" label={t('Dead-letter exchange')}><Input style={{ width: 180 }} /></Form.Item>
          <Form.Item name="ttl" label={t('Message TTL (ms)')}><InputNumber min={0} style={{ width: 150 }} /></Form.Item>
          <Form.Item name="maxLength" label={t('Max length')}><InputNumber min={0} style={{ width: 150 }} /></Form.Item>
        </Space>
        <Space>
          <Form.Item name="bindExchange" label={t('Bind to exchange')}>
            <Select allowClear showSearch style={{ width: 260 }} options={exchanges.filter((x) => x.name && !x.name.startsWith('amq.')).map((x) => ({ value: x.name }))} />
          </Form.Item>
          <Form.Item name="routingKey" label="Routing key"><Input style={{ width: 260 }} /></Form.Item>
        </Space>
      </Form>
      <CopyCommand cmd={cmd} />
    </Modal>
  )
}

function CreateExchangeModal({ inst, open, onClose, onDone }: { inst: Instance; open: boolean; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  const submit = async () => {
    const x = await form.validateFields()
    try {
      await api.rabbitCreateExchange(inst.id, x)
      message.success(t('Created {name}', { name: x.name }))
      onClose()
      onDone()
    } catch (e) {
      message.error(errMsg(e))
    }
  }
  return (
    <Modal title={t('New exchange')} open={open} onCancel={onClose} onOk={submit} okText={t('Create')} cancelText={t('Cancel')} destroyOnClose>
      <Form form={form} layout="vertical" requiredMark={false} initialValues={{ type: 'topic', durable: true }}>
        <Form.Item name="name" label={t('Name')} rules={[{ required: true, message: t('Required') }]}><Input /></Form.Item>
        <Form.Item name="type" label={t('Type')}><Select options={['direct', 'topic', 'fanout', 'headers'].map((x) => ({ value: x }))} /></Form.Item>
        <Form.Item name="durable" valuePropName="checked"><Checkbox>{t('Durable')}</Checkbox></Form.Item>
      </Form>
    </Modal>
  )
}

function PublishModal({ inst, init, exchanges, onClose }: { inst: Instance; init: { exchange: string; routingKey: string }; exchanges: Exchange[]; onClose: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm()
  const submit = async () => {
    const x = await form.validateFields()
    const headers: Record<string, string> = {}
    for (const line of (x.headers ?? '').split('\n')) {
      const [k, ...rest] = line.split(':')
      if (k?.trim()) headers[k.trim()] = rest.join(':').trim()
    }
    try {
      const routed = await api.rabbitPublish(inst.id, { exchange: x.exchange ?? '', routingKey: x.routingKey ?? '', payload: x.payload ?? '', headers, persistent: x.persistent })
      if (routed) message.success(t('Published and routed to a queue'))
      else message.warning(t('Published, but no queue matched the routing key (message dropped)'))
    } catch (e) {
      message.error(errMsg(e))
    }
  }
  return (
    <Modal title={t('Publish message')} open onCancel={onClose} onOk={submit} okText={t('Publish')} cancelText={t('Close')} width={660}>
      <Form form={form} layout="vertical" requiredMark={false} initialValues={{ ...init, persistent: true }}>
        <Space>
          <Form.Item name="exchange" label={t('Exchange')}>
            <Select showSearch allowClear style={{ width: 280 }} placeholder="(default)" options={exchanges.filter((x) => !x.internal).map((x) => ({ value: x.name, label: x.name || '(default)' }))} />
          </Form.Item>
          <Form.Item name="routingKey" label="Routing key"><Input style={{ width: 280 }} /></Form.Item>
        </Space>
        <Form.Item name="payload" label="Payload"><Input.TextArea rows={8} className="mono" /></Form.Item>
        <Form.Item name="headers" label={t('Headers (one "key: value" per line)')}><Input.TextArea rows={2} className="mono" /></Form.Item>
        <Form.Item name="persistent" valuePropName="checked"><Checkbox>{t('Persistent (delivery_mode=2)')}</Checkbox></Form.Item>
      </Form>
    </Modal>
  )
}
