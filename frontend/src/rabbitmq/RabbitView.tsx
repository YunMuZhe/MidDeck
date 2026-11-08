import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, App, Button, Checkbox, Drawer, Form, Input, InputNumber, Modal, Select, Space, Table, Tabs, Tag, Typography } from 'antd'
import { ExportOutlined, PlusOutlined, ReloadOutlined, SendOutlined } from '@ant-design/icons'
import { api, errMsg } from '../api'
import { CopyCommand, prettyJSON, useDangerConfirm } from '../common'
import type { Binding, Exchange, Instance, PeekedMessage, Queue } from '../types'

export default function RabbitView({ inst }: { inst: Instance }) {
  const { message } = App.useApp()
  const [queues, setQueues] = useState<Queue[]>([])
  const [exchanges, setExchanges] = useState<Exchange[]>([])
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [peek, setPeek] = useState<Queue | null>(null)
  const [createQ, setCreateQ] = useState(false)
  const [createX, setCreateX] = useState(false)
  const [publish, setPublish] = useState<{ exchange: string; routingKey: string } | null>(null)
  const { confirm, ctx } = useDangerConfirm(inst)

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

  const qRows = useMemo(() => queues.filter((x) => x.name.toLowerCase().includes(q.toLowerCase())), [queues, q])
  const xRows = useMemo(() => exchanges.filter((x) => x.name.toLowerCase().includes(q.toLowerCase())), [exchanges, q])
  const danger = (title: string, name: string, fn: () => Promise<void>) =>
    confirm(title, name, async () => {
      try {
        await fn()
        message.success('完成')
        load()
      } catch (e) {
        message.error(errMsg(e))
        throw e
      }
    })

  const toolbar = (
    <Space style={{ marginBottom: 12 }} wrap>
      <Input.Search placeholder="过滤名称" allowClear value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 260 }} />
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
      <Button icon={<PlusOutlined />} type="primary" disabled={inst.readOnly} onClick={() => setCreateQ(true)}>新建队列</Button>
      <Button icon={<PlusOutlined />} disabled={inst.readOnly} onClick={() => setCreateX(true)}>新建 Exchange</Button>
      <Button icon={<SendOutlined />} disabled={inst.readOnly} onClick={() => setPublish({ exchange: '', routingKey: '' })}>发布消息</Button>
      <Button icon={<ExportOutlined />} onClick={async () => {
        try { const p = await api.rabbitExportDefinitions(inst.id); if (p) message.success('已导出到 ' + p) } catch (e) { message.error(errMsg(e)) }
      }}>导出 definitions</Button>
    </Space>
  )

  return (
    <>
      {ctx}
      <Tabs
        items={[
          {
            key: 'q', label: `队列 (${queues.length})`,
            children: (
              <>
                {toolbar}
                <Table size="small" rowKey="name" loading={loading} dataSource={qRows} pagination={{ pageSize: 50 }}
                  columns={[
                    { title: '队列', dataIndex: 'name', render: (n, r) => <a onClick={() => setPeek(r)}>{n}</a>, sorter: (a, b) => a.name.localeCompare(b.name) },
                    { title: '类型', dataIndex: 'type', width: 90, render: (t) => <Tag>{t}</Tag> },
                    { title: '状态', dataIndex: 'state', width: 90, render: (s) => <Tag color={s === 'running' ? 'green' : 'orange'}>{s}</Tag> },
                    { title: 'Ready', dataIndex: 'messages_ready', width: 100, sorter: (a, b) => a.messages_ready - b.messages_ready, render: (n) => <b style={{ color: n > 0 ? '#f08c00' : undefined }}>{n}</b> },
                    { title: 'Unacked', dataIndex: 'messages_unacknowledged', width: 100 },
                    { title: '消费者', dataIndex: 'consumers', width: 80, render: (c) => <span style={{ color: c === 0 ? '#e03131' : undefined }}>{c}</span> },
                    {
                      title: '操作', width: 200,
                      render: (_, r) => (
                        <Space>
                          <a onClick={() => setPeek(r)}>查看消息</a>
                          {!inst.readOnly && <a onClick={() => danger('清空队列（消息不可恢复）', r.name, () => api.rabbitPurgeQueue(inst.id, r.name))}>清空</a>}
                          {!inst.readOnly && <a style={{ color: '#e03131' }} onClick={() => danger('删除队列', r.name, () => api.rabbitDeleteQueue(inst.id, r.name))}>删除</a>}
                        </Space>
                      ),
                    },
                  ]} />
              </>
            ),
          },
          {
            key: 'x', label: `Exchange (${exchanges.length})`,
            children: (
              <>
                {toolbar}
                <Table size="small" rowKey="name" loading={loading} dataSource={xRows} pagination={{ pageSize: 50 }}
                  columns={[
                    { title: 'Exchange', dataIndex: 'name', render: (n) => n || <Typography.Text type="secondary">(AMQP default)</Typography.Text> },
                    { title: '类型', dataIndex: 'type', render: (t) => <Tag>{t}</Tag> },
                    { title: '持久化', dataIndex: 'durable', render: (d) => (d ? '是' : '否') },
                    { title: '内部', dataIndex: 'internal', render: (d) => (d ? '是' : '') },
                    { title: '操作', render: (_, r) => !inst.readOnly && !r.internal && <a onClick={() => setPublish({ exchange: r.name, routingKey: '' })}>发布消息</a> },
                  ]} />
              </>
            ),
          },
        ]}
      />
      {peek && <PeekDrawer inst={inst} queue={peek} onClose={() => setPeek(null)} />}
      <CreateQueueModal inst={inst} open={createQ} exchanges={exchanges} onClose={() => setCreateQ(false)} onDone={load} />
      <CreateExchangeModal inst={inst} open={createX} onClose={() => setCreateX(false)} onDone={load} />
      {publish && <PublishModal inst={inst} init={publish} exchanges={exchanges} onClose={() => setPublish(null)} />}
    </>
  )
}

function PeekDrawer({ inst, queue, onClose }: { inst: Instance; queue: Queue; onClose: () => void }) {
  const { message } = App.useApp()
  const [n, setN] = useState(20)
  const [msgs, setMsgs] = useState<PeekedMessage[]>([])
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
    <Drawer title={queue.name} width={860} open onClose={onClose}>
      <Space direction="vertical" style={{ width: '100%' }}>
        <Space wrap>
          <Tag>{queue.type}</Tag>
          <span>Ready <b>{queue.messages_ready}</b></span>
          <span>Unacked {queue.messages_unacknowledged}</span>
          <span>消费者 {queue.consumers}</span>
          {queue.arguments && Object.entries(queue.arguments).map(([k, v]) => <Tag key={k}>{k}={String(v)}</Tag>)}
        </Space>
        <Typography.Text strong>绑定</Typography.Text>
        <Table size="small" rowKey={(b) => b.source + b.routing_key} pagination={false} dataSource={bindings}
          columns={[{ title: 'Exchange', dataIndex: 'source', render: (s) => s || '(default)' }, { title: 'Routing Key', dataIndex: 'routing_key' }]} />
        <Alert type="warning" showIcon message="查看消息会取出后立即重新入队（requeue）：消息会被标记为 redelivered，经典队列中位置可能变化。" />
        <Space>
          <InputNumber min={1} max={500} value={n} onChange={(v) => setN(v ?? 20)} addonBefore="条数" />
          <Button type="primary" onClick={load} loading={loading}>获取消息</Button>
        </Space>
        <Table size="small" rowKey={(_, i) => String(i)} dataSource={msgs} pagination={false}
          expandable={{ expandedRowRender: (m) => <pre className="mono">{prettyJSON(m.payload)}{'\n\nproperties: ' + JSON.stringify(m.properties, null, 2)}</pre> }}
          columns={[
            { title: 'Exchange', dataIndex: 'exchange', width: 160 },
            { title: 'Routing Key', dataIndex: 'routing_key', width: 160 },
            { title: 'Payload', dataIndex: 'payload', render: (p) => <span className="mono cell-ellipsis" style={{ maxWidth: 360 }}>{p}</span> },
            { title: '大小', dataIndex: 'payload_bytes', width: 80 },
            { title: '重投', dataIndex: 'redelivered', width: 60, render: (r) => (r ? '是' : '') },
          ]} />
      </Space>
    </Drawer>
  )
}

function CreateQueueModal({ inst, open, exchanges, onClose, onDone }: { inst: Instance; open: boolean; exchanges: Exchange[]; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
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
      message.success('已创建')
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
    <Modal title="新建队列" open={open} onCancel={onClose} onOk={submit} width={600} destroyOnClose>
      <Form form={form} layout="vertical" initialValues={{ type: 'quorum', durable: true }}>
        <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
        <Space>
          <Form.Item name="type" label="类型">
            <Select style={{ width: 160 }} options={[{ value: 'classic' }, { value: 'quorum' }, { value: 'stream' }]} />
          </Form.Item>
          <Form.Item name="durable" label=" " valuePropName="checked"><Checkbox>持久化</Checkbox></Form.Item>
        </Space>
        <Space>
          <Form.Item name="dlx" label="死信 Exchange"><Input style={{ width: 180 }} /></Form.Item>
          <Form.Item name="ttl" label="消息 TTL (ms)"><InputNumber min={0} style={{ width: 150 }} /></Form.Item>
          <Form.Item name="maxLength" label="最大长度"><InputNumber min={0} style={{ width: 150 }} /></Form.Item>
        </Space>
        <Space>
          <Form.Item name="bindExchange" label="同时绑定到 Exchange">
            <Select allowClear showSearch style={{ width: 240 }} options={exchanges.filter((x) => x.name && !x.name.startsWith('amq.')).map((x) => ({ value: x.name }))} />
          </Form.Item>
          <Form.Item name="routingKey" label="Routing Key"><Input style={{ width: 240 }} /></Form.Item>
        </Space>
      </Form>
      <CopyCommand cmd={cmd} />
    </Modal>
  )
}

function CreateExchangeModal({ inst, open, onClose, onDone }: { inst: Instance; open: boolean; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const [form] = Form.useForm()
  const submit = async () => {
    const x = await form.validateFields()
    try {
      await api.rabbitCreateExchange(inst.id, x)
      message.success('已创建')
      onClose()
      onDone()
    } catch (e) {
      message.error(errMsg(e))
    }
  }
  return (
    <Modal title="新建 Exchange" open={open} onCancel={onClose} onOk={submit} destroyOnClose>
      <Form form={form} layout="vertical" initialValues={{ type: 'topic', durable: true }}>
        <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
        <Form.Item name="type" label="类型"><Select options={['direct', 'topic', 'fanout', 'headers'].map((v) => ({ value: v }))} /></Form.Item>
        <Form.Item name="durable" valuePropName="checked"><Checkbox>持久化</Checkbox></Form.Item>
      </Form>
    </Modal>
  )
}

function PublishModal({ inst, init, exchanges, onClose }: { inst: Instance; init: { exchange: string; routingKey: string }; exchanges: Exchange[]; onClose: () => void }) {
  const { message } = App.useApp()
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
      if (routed) message.success('已发布并路由到队列')
      else message.warning('已发布，但没有任何队列匹配该 routing key（消息被丢弃）')
    } catch (e) {
      message.error(errMsg(e))
    }
  }
  return (
    <Modal title="发布消息" open onCancel={onClose} onOk={submit} okText="发布" width={640}>
      <Form form={form} layout="vertical" initialValues={{ ...init, persistent: true }}>
        <Space>
          <Form.Item name="exchange" label="Exchange">
            <Select showSearch allowClear style={{ width: 260 }} placeholder="(default)" options={exchanges.filter((x) => !x.internal).map((x) => ({ value: x.name, label: x.name || '(default)' }))} />
          </Form.Item>
          <Form.Item name="routingKey" label="Routing Key"><Input style={{ width: 260 }} /></Form.Item>
        </Space>
        <Form.Item name="payload" label="Payload"><Input.TextArea rows={8} className="mono" /></Form.Item>
        <Form.Item name="headers" label="Headers（每行 key: value）"><Input.TextArea rows={2} className="mono" /></Form.Item>
        <Form.Item name="persistent" valuePropName="checked"><Checkbox>持久化消息（delivery_mode=2）</Checkbox></Form.Item>
      </Form>
    </Modal>
  )
}
