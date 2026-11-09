import { useCallback, useEffect, useRef, useState } from 'react'
import { App, Button, Descriptions, Empty, Input, InputNumber, Space, Table, Tabs, Tag, Typography } from 'antd'
import { DeleteOutlined, ReloadOutlined, SaveOutlined, SearchOutlined } from '@ant-design/icons'
import { api, errMsg } from '../api'
import { fmtBytes, fmtTime, num, prettyJSON, Tiles, useDangerConfirm } from '../common'
import { useT } from '../settings'
import type { Intent, Instance, KeyItem, KeyValue, SlowLog } from '../types'

const typeColor: Record<string, string> = { string: 'blue', hash: 'purple', list: 'green', set: 'orange', zset: 'magenta', stream: 'cyan' }

const keyCount = (info: Record<string, string>) =>
  Object.entries(info)
    .filter(([k]) => /^db\d+$/.test(k))
    .reduce((s, [, v]) => s + Number(/keys=(\d+)/.exec(v)?.[1] ?? 0), 0)

export default function RedisView({ inst, intent, onIntentDone }: { inst: Instance; intent?: Intent; onIntentDone: () => void }) {
  const t = useT()
  const [tab, setTab] = useState('keys')
  const [pattern, setPattern] = useState<{ v: string; seq: number }>()
  const [command, setCommand] = useState<{ v: string; seq: number }>()

  // Proposals from the AI assistant: fill in the pattern or command, never run it.
  useEffect(() => {
    if (!intent) return
    const a = intent.args
    switch (intent.action) {
      case 'open_instance':
        if (['keys', 'console', 'info'].includes(a.tab)) setTab(a.tab)
        break
      case 'redis_scan':
        setTab('keys')
        setPattern({ v: a.pattern ?? '*', seq: intent.seq })
        break
      case 'redis_command':
        setTab('console')
        setCommand({ v: a.command ?? '', seq: intent.seq })
        break
    }
    onIntentDone()
  }, [intent, onIntentDone])
  const [info, setInfo] = useState<Record<string, string> | null>(null)
  const loadInfo = useCallback(() => {
    api.redisOverview(inst.id).then(setInfo).catch(() => {})
  }, [inst.id])
  useEffect(() => {
    loadInfo()
  }, [loadInfo])

  const hits = Number(info?.keyspace_hits ?? 0)
  const misses = Number(info?.keyspace_misses ?? 0)
  return (
    <>
      <Tiles
        items={[
          { label: t('Memory used'), value: info ? fmtBytes(Number(info.used_memory ?? 0)) : '-', note: info?.maxmemory && info.maxmemory !== '0' ? t('of {m}', { m: fmtBytes(Number(info.maxmemory)) }) : t('No maxmemory limit') },
          { label: t('Keys'), value: info ? num(keyCount(info)) : '-' },
          { label: t('Clients'), value: info ? num(Number(info.connected_clients)) : '-' },
          { label: t('Ops/sec'), value: info ? num(Number(info.instantaneous_ops_per_sec)) : '-' },
          { label: t('Hit rate'), value: hits + misses ? `${((hits / (hits + misses)) * 100).toFixed(1)}%` : '-', note: info ? t('{d} days uptime', { d: info.uptime_in_days }) : undefined },
        ]}
      />
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          { key: 'keys', label: t('Keys'), children: <KeysTab inst={inst} onChanged={loadInfo} prefill={pattern} /> },
          { key: 'console', label: t('Console'), children: <ConsoleTab inst={inst} prefill={command} /> },
          { key: 'info', label: t('Server info'), children: <InfoTab inst={inst} info={info} onRefresh={loadInfo} /> },
        ]}
      />
    </>
  )
}

function KeysTab({ inst, onChanged, prefill }: { inst: Instance; onChanged: () => void; prefill?: { v: string; seq: number } }) {
  const { message } = App.useApp()
  const t = useT()
  const [pattern, setPattern] = useState(prefill?.v ?? '*')
  const [keys, setKeys] = useState<KeyItem[]>([])
  const [cursor, setCursor] = useState(0)
  const [done, setDone] = useState(true)
  const [loading, setLoading] = useState(false)
  const [sel, setSel] = useState<string[]>([])
  const [current, setCurrent] = useState<KeyValue | null>(null)
  const confirm = useDangerConfirm(inst)

  // A pattern from the assistant replaces the box and clears stale results; SCAN runs it.
  useEffect(() => {
    if (!prefill) return
    setPattern(prefill.v)
    setKeys([])
    setDone(true)
    setCurrent(null)
  }, [prefill])

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
    confirm(t('Delete {n} key(s)', { n: list.length }), list.length === 1 ? list[0] : `${list.length} keys`, async () => {
      try {
        const n = await api.redisDelete(inst.id, list)
        message.success(t('Deleted {n}', { n }))
        setKeys((ks) => ks.filter((k) => !list.includes(k.key)))
        setSel([])
        if (current && list.includes(current.key)) setCurrent(null)
        onChanged()
      } catch (e) {
        message.error(errMsg(e))
        throw e
      }
    })

  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(320px, 2fr) 3fr', gap: 20 }}>
      <div>
        <Space.Compact style={{ width: '100%', marginBottom: 10 }}>
          <Input prefix={<SearchOutlined className="muted" />} value={pattern} onChange={(e) => setPattern(e.target.value)} onPressEnter={() => scan(false)} placeholder="user:*" className="mono" />
          <Button type="primary" onClick={() => scan(false)} loading={loading}>SCAN</Button>
        </Space.Compact>
        <div className="toolbar" style={{ minHeight: 24 }}>
          <span className="muted">{done ? t('{n} keys (scan complete)', { n: keys.length }) : t('{n} keys loaded', { n: keys.length })}</span>
          {!done && <Button size="small" onClick={() => scan(true)} loading={loading}>{t('Scan more')}</Button>}
          <div className="spacer" />
          {sel.length > 0 && !inst.readOnly && <Button size="small" danger onClick={() => del(sel)}>{t('Delete selected ({n})', { n: sel.length })}</Button>}
        </div>
        <Table
          size="small"
          rowKey="key"
          dataSource={keys}
          loading={loading}
          pagination={{ pageSize: 100, size: 'small', hideOnSinglePage: true }}
          scroll={{ y: 'calc(100vh - 420px)' }}
          rowSelection={inst.readOnly ? undefined : { selectedRowKeys: sel, onChange: (k) => setSel(k as string[]), columnWidth: 32 }}
          onRow={(r) => ({ onClick: () => open(r.key), style: { cursor: 'pointer' } })}
          rowClassName={(r) => (r.key === current?.key ? 'ant-table-row-selected' : '')}
          columns={[
            { title: 'Key', dataIndex: 'key', ellipsis: true, render: (k) => <span className="mono">{k}</span> },
            { title: t('Type'), dataIndex: 'type', width: 76, render: (x) => <Tag color={typeColor[x]} bordered={false}>{x}</Tag> },
            { title: 'TTL', dataIndex: 'ttl', width: 70, align: 'right', render: (x) => (x < 0 ? '∞' : `${x}s`) },
          ]}
        />
      </div>
      <div style={{ minWidth: 0 }}>
        {current ? (
          <ValuePane inst={inst} v={current} onReload={() => open(current.key)} onDelete={() => del([current.key])} />
        ) : (
          <Empty style={{ marginTop: 100 }} description={t('Select a key to view its value')} />
        )}
      </div>
    </div>
  )
}

function ValuePane({ inst, v, onReload, onDelete }: { inst: Instance; v: KeyValue; onReload: () => void; onDelete: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [text, setText] = useState(v.string ?? '')
  const [ttl, setTtl] = useState<number | null>(v.ttl)
  useEffect(() => {
    setText(v.string ?? '')
    setTtl(v.ttl)
  }, [v])

  return (
    <Space direction="vertical" style={{ width: '100%' }} size={12}>
      <div className="toolbar" style={{ marginBottom: 0 }}>
        <Tag color={typeColor[v.type]} bordered={false}>{v.type}</Tag>
        <Typography.Text strong copyable className="mono" style={{ fontSize: 14 }}>{v.key}</Typography.Text>
        <div className="spacer" />
        <Button size="small" icon={<ReloadOutlined />} onClick={onReload} />
        {!inst.readOnly && <Button size="small" danger icon={<DeleteOutlined />} onClick={onDelete} />}
      </div>
      <div className="toolbar" style={{ marginBottom: 0 }}>
        <span className="muted">{t('Size')} {num(v.size)}</span>
        {v.memory > 0 && <span className="muted">· {t('Memory')} {fmtBytes(v.memory)}</span>}
        <div className="spacer" />
        <InputNumber size="small" addonBefore="TTL" addonAfter={t('sec')} value={ttl ?? undefined} placeholder={t('-1 = never')} onChange={(n) => setTtl(n)} style={{ width: 210 }} disabled={inst.readOnly} />
        {!inst.readOnly && (
          <Button size="small" onClick={async () => {
            try { await api.redisSetTTL(inst.id, v.key, ttl ?? -1); message.success(t('TTL updated')); onReload() } catch (e) { message.error(errMsg(e)) }
          }}>{t('Set TTL')}</Button>
        )}
      </div>
      {v.type === 'string' ? (
        <>
          <Input.TextArea className="mono" value={text} onChange={(e) => setText(e.target.value)} autoSize={{ minRows: 14, maxRows: 32 }} readOnly={inst.readOnly} />
          <Space>
            <Button size="small" onClick={() => setText(prettyJSON(text))}>{t('Format JSON')}</Button>
            {!inst.readOnly && (
              <Button size="small" type="primary" icon={<SaveOutlined />} onClick={async () => {
                try { await api.redisSetString(inst.id, v.key, text, v.ttl > 0 ? v.ttl : 0); message.success(t('Saved')); onReload() } catch (e) { message.error(errMsg(e)) }
              }}>{t('Save')}</Button>
            )}
          </Space>
        </>
      ) : (
        <>
          {v.truncated && <Typography.Text type="warning">{t('Showing the first {a} of {b}', { a: v.items?.length ?? 0, b: v.size })}</Typography.Text>}
          <Table
            size="small"
            rowKey={(r) => `${r.id ?? ''}|${r.field ?? ''}|${r.value}`}
            dataSource={v.items ?? []}
            pagination={{ pageSize: 50, size: 'small', hideOnSinglePage: true }}
            columns={[
              ...(v.type === 'stream' ? [{ title: 'ID', dataIndex: 'id', width: 190, render: (x: string) => <span className="mono">{x}</span> }] : []),
              ...(v.type === 'hash' || v.type === 'list'
                ? [{ title: v.type === 'list' ? '#' : 'Field', dataIndex: 'field', width: v.type === 'list' ? 60 : 200, render: (f: string) => <span className="mono">{f}</span> }]
                : []),
              { title: t('Value'), dataIndex: 'value', render: (x: string) => <Typography.Paragraph className="mono" style={{ marginBottom: 0 }} ellipsis={{ rows: 3, expandable: true }} copyable>{x}</Typography.Paragraph> },
              ...(v.type === 'zset' ? [{ title: 'Score', dataIndex: 'score', width: 120, align: 'right' as const }] : []),
            ]}
          />
        </>
      )}
    </Space>
  )
}

interface Line { kind: 'cmd' | 'out' | 'err'; text: string }

function ConsoleTab({ inst, prefill }: { inst: Instance; prefill?: { v: string; seq: number } }) {
  const { modal } = App.useApp()
  const t = useT()
  const [lines, setLines] = useState<Line[]>([])
  const [input, setInput] = useState(prefill?.v ?? '')
  useEffect(() => {
    if (prefill) setInput(prefill.v)
  }, [prefill])
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
          title: t('Run a dangerous command?'),
          content: <Typography.Text code>{cmd}</Typography.Text>,
          okType: 'danger',
          okText: t('Run'),
          cancelText: t('Cancel'),
          onOk: () => exec(cmd, true),
          onCancel: () => setLines((l) => [...l, { kind: 'err', text: t('Cancelled') }]),
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
        {lines.length === 0 && <div style={{ color: '#6e7681' }}>{t('Type a Redis command and press Enter. ↑/↓ for history, "clear" to clear. KEYS, FLUSHALL, DEL and similar ask for confirmation.')}</div>}
        {lines.map((l, i) => (
          <pre key={i} className={'mono ' + (l.kind === 'cmd' ? 'cmd' : l.kind === 'err' ? 'err' : '')}>{l.text}</pre>
        ))}
      </div>
      <Input
        className="mono console-in"
        value={input}
        placeholder="GET user:1:profile"
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
  ['redis_version', 'Version'], ['redis_mode', 'Mode'], ['role', 'Role'], ['os', 'OS'],
  ['used_memory_human', 'Memory used'], ['used_memory_peak_human', 'Peak memory'], ['maxmemory_policy', 'Eviction policy'],
  ['mem_fragmentation_ratio', 'Fragmentation'], ['evicted_keys', 'Evicted keys'], ['expired_keys', 'Expired keys'],
  ['aof_enabled', 'AOF'], ['rdb_last_save_time', 'Last RDB save'],
]

function InfoTab({ inst, info, onRefresh }: { inst: Instance; info: Record<string, string> | null; onRefresh: () => void }) {
  const t = useT()
  const [slow, setSlow] = useState<SlowLog[]>([])
  const loadSlow = useCallback(() => {
    api.redisSlowLog(inst.id, 50).then(setSlow).catch(() => {})
  }, [inst.id])
  useEffect(() => {
    loadSlow()
  }, [loadSlow])
  const dbs = Object.entries(info ?? {}).filter(([k]) => /^db\d+$/.test(k))
  return (
    <Space direction="vertical" style={{ width: '100%' }} size="large">
      <div className="toolbar" style={{ marginBottom: 0 }}>
        <div className="spacer" />
        <Button icon={<ReloadOutlined />} onClick={() => { onRefresh(); loadSlow() }}>{t('Refresh')}</Button>
      </div>
      <Descriptions size="small" bordered column={3}>
        {infoKeys.filter(([k]) => info?.[k] !== undefined).map(([k, label]) => (
          <Descriptions.Item key={k} label={t(label)}>
            {k === 'rdb_last_save_time' ? fmtTime(Number(info![k]) * 1000) : info![k]}
          </Descriptions.Item>
        ))}
        {dbs.map(([k, v]) => <Descriptions.Item key={k} label={k}><span className="mono">{v}</span></Descriptions.Item>)}
      </Descriptions>
      <div>
        <div className="section-title">{t('Slow log (SLOWLOG GET 50)')}</div>
        <Table
          size="small"
          rowKey="id"
          dataSource={slow}
          pagination={false}
          locale={{ emptyText: t('No slow commands recorded') }}
          columns={[
            { title: t('Time'), dataIndex: 'time', width: 180, render: (x) => fmtTime(x * 1000) },
            { title: t('Duration'), dataIndex: 'durationUs', width: 110, align: 'right', render: (d) => `${(d / 1000).toFixed(2)} ms` },
            { title: t('Command'), dataIndex: 'command', render: (c) => <span className="mono cell-ellipsis">{c}</span> },
            { title: t('Client'), dataIndex: 'client', width: 170 },
          ]}
        />
      </div>
    </Space>
  )
}
