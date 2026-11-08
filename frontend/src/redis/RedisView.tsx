import { useCallback, useEffect, useRef, useState } from 'react'
import { App, Button, Col, Descriptions, Empty, Input, InputNumber, List, Row, Space, Table, Tabs, Tag, Typography } from 'antd'
import { DeleteOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { api, errMsg } from '../api'
import { fmtTime, prettyJSON, useDangerConfirm } from '../common'
import type { Instance, KeyItem, KeyValue, SlowLog } from '../types'

const typeColor: Record<string, string> = { string: 'blue', hash: 'purple', list: 'green', set: 'orange', zset: 'magenta', stream: 'cyan' }

export default function RedisView({ inst }: { inst: Instance }) {
  return (
    <Tabs
      items={[
        { key: 'keys', label: 'Key 浏览', children: <KeysTab inst={inst} /> },
        { key: 'console', label: '命令行', children: <ConsoleTab inst={inst} /> },
        { key: 'info', label: '概览 / 慢日志', children: <InfoTab inst={inst} /> },
      ]}
    />
  )
}

function KeysTab({ inst }: { inst: Instance }) {
  const { message } = App.useApp()
  const [pattern, setPattern] = useState('*')
  const [keys, setKeys] = useState<KeyItem[]>([])
  const [cursor, setCursor] = useState(0)
  const [done, setDone] = useState(true)
  const [loading, setLoading] = useState(false)
  const [sel, setSel] = useState<string[]>([])
  const [current, setCurrent] = useState<KeyValue | null>(null)
  const { confirm, ctx } = useDangerConfirm(inst)

  const scan = useCallback(async (more = false) => {
    setLoading(true)
    try {
      const r = await api.redisScan(inst.id, pattern, more ? cursor : 0, 500)
      setKeys((prev) => (more ? [...prev, ...(r.keys ?? [])] : r.keys ?? []))
      setCursor(r.cursor)
      setDone(r.done)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [inst.id, pattern, cursor, message])

  useEffect(() => {
    scan(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inst.id])

  const open = async (k: string) => {
    try {
      setCurrent(await api.redisGet(inst.id, k))
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  const del = (list: string[]) =>
    confirm(`删除 ${list.length} 个 key`, list.length === 1 ? list[0] : `${list.length} keys`, async () => {
      try {
        const n = await api.redisDelete(inst.id, list)
        message.success(`已删除 ${n} 个`)
        setKeys((ks) => ks.filter((k) => !list.includes(k.key)))
        setSel([])
        if (current && list.includes(current.key)) setCurrent(null)
      } catch (e) {
        message.error(errMsg(e))
        throw e
      }
    })

  return (
    <Row gutter={16}>
      {ctx}
      <Col span={10}>
        <Space.Compact style={{ width: '100%', marginBottom: 8 }}>
          <Input value={pattern} onChange={(e) => setPattern(e.target.value)} onPressEnter={() => scan(false)} placeholder="匹配模式，如 user:*" className="mono" />
          <Button type="primary" onClick={() => scan(false)} loading={loading}>SCAN</Button>
        </Space.Compact>
        <Space style={{ marginBottom: 8 }}>
          <Typography.Text type="secondary">已加载 {keys.length} 个{done ? '（已扫描完毕）' : ''}</Typography.Text>
          {!done && <Button size="small" onClick={() => scan(true)} loading={loading}>继续扫描</Button>}
          {sel.length > 0 && !inst.readOnly && <Button size="small" danger onClick={() => del(sel)}>删除选中 ({sel.length})</Button>}
        </Space>
        <Table
          size="small"
          rowKey="key"
          dataSource={keys}
          loading={loading}
          pagination={{ pageSize: 100, size: 'small' }}
          scroll={{ y: 'calc(100vh - 330px)' }}
          rowSelection={inst.readOnly ? undefined : { selectedRowKeys: sel, onChange: (k) => setSel(k as string[]) }}
          onRow={(r) => ({ onClick: () => open(r.key), style: { cursor: 'pointer' } })}
          rowClassName={(r) => (r.key === current?.key ? 'ant-table-row-selected' : '')}
          columns={[
            { title: 'Key', dataIndex: 'key', render: (k) => <span className="mono">{k}</span> },
            { title: '类型', dataIndex: 'type', width: 80, render: (t) => <Tag color={typeColor[t]}>{t}</Tag> },
            { title: 'TTL', dataIndex: 'ttl', width: 80, render: (t) => (t < 0 ? '∞' : `${t}s`) },
          ]}
        />
      </Col>
      <Col span={14}>
        {current ? (
          <ValuePane inst={inst} v={current} onReload={() => open(current.key)} onDelete={() => del([current.key])} />
        ) : (
          <Empty style={{ marginTop: 80 }} description="选择一个 key 查看内容" />
        )}
      </Col>
    </Row>
  )
}

function ValuePane({ inst, v, onReload, onDelete }: { inst: Instance; v: KeyValue; onReload: () => void; onDelete: () => void }) {
  const { message } = App.useApp()
  const [text, setText] = useState(v.string ?? '')
  const [ttl, setTtl] = useState<number | null>(v.ttl)
  useEffect(() => {
    setText(v.string ?? '')
    setTtl(v.ttl)
  }, [v])

  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <Space wrap>
        <Tag color={typeColor[v.type]}>{v.type}</Tag>
        <Typography.Text strong copyable className="mono">{v.key}</Typography.Text>
      </Space>
      <Space wrap>
        <span>大小 {v.size.toLocaleString()}</span>
        {v.memory > 0 && <span>内存 {(v.memory / 1024).toFixed(1)} KB</span>}
        <InputNumber size="small" addonBefore="TTL" addonAfter="秒" value={ttl ?? undefined} placeholder="-1 永久" onChange={(n) => setTtl(n)} style={{ width: 200 }} disabled={inst.readOnly} />
        {!inst.readOnly && (
          <Button size="small" onClick={async () => {
            try { await api.redisSetTTL(inst.id, v.key, ttl ?? -1); message.success('TTL 已更新'); onReload() } catch (e) { message.error(errMsg(e)) }
          }}>设置 TTL</Button>
        )}
        <Button size="small" icon={<ReloadOutlined />} onClick={onReload} />
        {!inst.readOnly && <Button size="small" danger icon={<DeleteOutlined />} onClick={onDelete} />}
      </Space>
      {v.type === 'string' ? (
        <>
          <Input.TextArea className="mono" value={text} onChange={(e) => setText(e.target.value)} autoSize={{ minRows: 12, maxRows: 30 }} readOnly={inst.readOnly} />
          <Space>
            <Button size="small" onClick={() => setText(prettyJSON(text))}>格式化 JSON</Button>
            {!inst.readOnly && (
              <Button size="small" type="primary" icon={<SaveOutlined />} onClick={async () => {
                try { await api.redisSetString(inst.id, v.key, text, v.ttl > 0 ? v.ttl : 0); message.success('已保存'); onReload() } catch (e) { message.error(errMsg(e)) }
              }}>保存</Button>
            )}
          </Space>
        </>
      ) : (
        <>
          {v.truncated && <Typography.Text type="warning">仅显示前 {v.items?.length ?? 0} 项（共 {v.size}）</Typography.Text>}
          <Table
            size="small"
            rowKey={(r, i) => `${r.id ?? r.field ?? ''}-${i}`}
            dataSource={v.items ?? []}
            pagination={{ pageSize: 50, size: 'small' }}
            columns={[
              ...(v.type === 'stream' ? [{ title: 'ID', dataIndex: 'id', width: 180 }] : []),
              ...(v.type === 'hash' || v.type === 'list' ? [{ title: v.type === 'list' ? '#' : 'Field', dataIndex: 'field', width: v.type === 'list' ? 60 : 200, render: (f: string) => <span className="mono">{f}</span> }] : []),
              { title: 'Value', dataIndex: 'value', render: (x: string) => <Typography.Paragraph className="mono" style={{ marginBottom: 0 }} ellipsis={{ rows: 3, expandable: true }} copyable>{x}</Typography.Paragraph> },
              ...(v.type === 'zset' ? [{ title: 'Score', dataIndex: 'score', width: 120 }] : []),
            ]}
          />
        </>
      )}
    </Space>
  )
}

interface Line { kind: 'cmd' | 'out' | 'err'; text: string }

function ConsoleTab({ inst }: { inst: Instance }) {
  const { modal } = App.useApp()
  const [lines, setLines] = useState<Line[]>([])
  const [input, setInput] = useState('')
  const [history, setHistory] = useState<string[]>([])
  const [hIdx, setHIdx] = useState(-1)
  const outRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    outRef.current?.scrollTo(0, outRef.current.scrollHeight)
  }, [lines])

  const exec = async (cmd: string, confirmed = false) => {
    try {
      const out = await api.redisExec(inst.id, cmd, confirmed)
      setLines((l) => [...l, { kind: 'out', text: out }])
    } catch (e) {
      const m = errMsg(e)
      if (m.startsWith('NEED_CONFIRM')) {
        modal.confirm({
          title: '确认执行危险命令？',
          content: <Typography.Text code>{cmd}</Typography.Text>,
          okType: 'danger',
          onOk: () => exec(cmd, true),
          onCancel: () => setLines((l) => [...l, { kind: 'err', text: '已取消' }]),
        })
        return
      }
      setLines((l) => [...l, { kind: 'err', text: '(error) ' + m }])
    }
  }

  const submit = () => {
    const cmd = input.trim()
    if (!cmd) return
    setLines((l) => [...l, { kind: 'cmd', text: `${inst.name}> ${cmd}` }])
    setHistory((h) => [cmd, ...h.filter((x) => x !== cmd)].slice(0, 100))
    setHIdx(-1)
    setInput('')
    if (cmd === 'clear') {
      setLines([])
      return
    }
    exec(cmd)
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <div className="console-out mono" ref={outRef}>
        {lines.length === 0 && <div style={{ color: '#888' }}>输入 Redis 命令，回车执行；↑↓ 切换历史；clear 清屏。KEYS、FLUSHALL、DEL 等命令需二次确认。</div>}
        {lines.map((l, i) => (
          <pre key={i} className={'mono ' + (l.kind === 'cmd' ? 'cmd' : l.kind === 'err' ? 'err' : '')}>{l.text}</pre>
        ))}
      </div>
      <Input
        className="mono"
        value={input}
        placeholder="GET user:1"
        onChange={(e) => setInput(e.target.value)}
        onPressEnter={submit}
        onKeyDown={(e) => {
          if (e.key === 'ArrowUp' && history.length) {
            const i = Math.min(hIdx + 1, history.length - 1)
            setHIdx(i)
            setInput(history[i])
            e.preventDefault()
          } else if (e.key === 'ArrowDown') {
            const i = hIdx - 1
            setHIdx(Math.max(i, -1))
            setInput(i >= 0 ? history[i] : '')
            e.preventDefault()
          }
        }}
      />
    </Space>
  )
}

const infoKeys: [string, string][] = [
  ['redis_version', '版本'], ['redis_mode', '模式'], ['role', '角色'], ['connected_clients', '连接数'],
  ['used_memory_human', '已用内存'], ['maxmemory_human', '最大内存'], ['maxmemory_policy', '淘汰策略'],
  ['mem_fragmentation_ratio', '碎片率'], ['instantaneous_ops_per_sec', 'QPS'], ['keyspace_hits', '命中'],
  ['keyspace_misses', '未命中'], ['evicted_keys', '淘汰 key'], ['expired_keys', '过期 key'], ['uptime_in_days', '运行天数'],
  ['aof_enabled', 'AOF'], ['rdb_last_save_time', '最近 RDB'],
]

function InfoTab({ inst }: { inst: Instance }) {
  const { message } = App.useApp()
  const [info, setInfo] = useState<Record<string, string>>({})
  const [slow, setSlow] = useState<SlowLog[]>([])
  const load = useCallback(async () => {
    try {
      setInfo(await api.redisOverview(inst.id))
      setSlow(await api.redisSlowLog(inst.id, 50))
    } catch (e) {
      message.error(errMsg(e))
    }
  }, [inst.id, message])
  useEffect(() => {
    load()
  }, [load])
  const dbs = Object.entries(info).filter(([k]) => /^db\d+$/.test(k))
  return (
    <Space direction="vertical" style={{ width: '100%' }} size="large">
      <Button icon={<ReloadOutlined />} onClick={load}>刷新</Button>
      <Descriptions size="small" bordered column={4}>
        {infoKeys.filter(([k]) => info[k] !== undefined).map(([k, label]) => (
          <Descriptions.Item key={k} label={label}>
            {k === 'rdb_last_save_time' ? fmtTime(Number(info[k]) * 1000) : info[k]}
          </Descriptions.Item>
        ))}
      </Descriptions>
      {dbs.length > 0 && (
        <List size="small" bordered header="Keyspace" dataSource={dbs} renderItem={([k, v]) => <List.Item><b>{k}</b>&nbsp;<span className="mono">{v}</span></List.Item>} />
      )}
      <Table
        size="small"
        rowKey="id"
        title={() => '慢日志（SLOWLOG GET 50）'}
        dataSource={slow}
        pagination={false}
        columns={[
          { title: '时间', dataIndex: 'time', width: 170, render: (t) => fmtTime(t * 1000) },
          { title: '耗时', dataIndex: 'durationUs', width: 110, render: (d) => `${(d / 1000).toFixed(2)} ms` },
          { title: '命令', dataIndex: 'command', render: (c) => <span className="mono cell-ellipsis">{c}</span> },
          { title: '客户端', dataIndex: 'client', width: 160 },
        ]}
      />
    </Space>
  )
}
