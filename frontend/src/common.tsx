import type { ReactNode } from 'react'
import dayjs from 'dayjs'
import { Alert, App, Input, Space, Tag, Tooltip, Typography } from 'antd'
import { CheckCircleFilled, CloseCircleFilled, CopyOutlined, ExclamationCircleFilled, MinusCircleFilled } from '@ant-design/icons'
import { useSettings, useT } from './settings'
import { kindColor, kindGlyph, kindLabel } from './theme'
import type { CompatResult, Instance, Kind, Report } from './types'

export { kindLabel }

const envClass = (env: string) =>
  ({ prod: 'prod', production: 'prod', staging: 'staging', test: 'test', dev: 'dev' })[env] ?? 'other'

export const isProd = (env: string) => env === 'prod' || env === 'production'

export function EnvTag({ env }: { env: string }) {
  return <span className={`env-pill ${envClass(env)}`}>{env || 'default'}</span>
}

export function KindIcon({ kind, size = 20 }: { kind: Kind; size?: number }) {
  const { dark } = useSettings()
  const glyph = kindGlyph[kind] ?? '?'
  return (
    <span
      className="kind-icon"
      title={kindLabel[kind]}
      style={{ width: size, height: size, background: kindColor(kind, dark), fontSize: size * (glyph.length > 1 ? 0.42 : 0.55), borderRadius: size * 0.28 }}
    >
      {glyph}
    </span>
  )
}

const statusIcon = {
  ok: <CheckCircleFilled style={{ color: '#2f9e44' }} />,
  warn: <ExclamationCircleFilled style={{ color: '#f08c00' }} />,
  fail: <CloseCircleFilled style={{ color: '#e03131' }} />,
  skip: <MinusCircleFilled style={{ color: '#adb5bd' }} />,
}

export function DiagReport({ report }: { report?: Report | null }) {
  const t = useT()
  if (!report) return null
  const warns = report.steps.filter((s) => s.status === 'warn').length
  return (
    <Space direction="vertical" style={{ width: '100%' }} size={10}>
      <Alert
        type={report.ok ? (warns ? 'warning' : 'success') : 'error'}
        showIcon
        message={report.ok ? (warns ? t('Connected with {n} warning(s)', { n: warns }) : t('All checks passed')) : t('Connection failed')}
      />
      <div>
        {report.steps.map((s, i) => (
          <div className="diag-step" key={i}>
            <span className="ic">{statusIcon[s.status]}</span>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div>
                <span className="nm">{s.name}</span>
                {s.durationMs > 0 && <span className="ms">{s.durationMs} ms</span>}
              </div>
              {s.detail && <div className="dt selectable">{s.detail}</div>}
              {s.hint && <div className="hint">{s.hint}</div>}
            </div>
          </div>
        ))}
      </div>
    </Space>
  )
}

const levelColor: Record<string, string> = { tested: 'success', supported: 'processing', partial: 'warning', unsupported: 'error', unknown: 'default' }
const levelText: Record<string, string> = { tested: 'Tested', supported: 'Supported', partial: 'Partial', unsupported: 'Unsupported', unknown: 'Unknown version' }

export function CompatBadge({ c }: { c?: CompatResult }) {
  const t = useT()
  if (!c) return null
  const tip = (
    <div>
      <div>{c.message}</div>
      {!!c.unavailable?.length && <div style={{ marginTop: 6 }}>{t('Unavailable')}: {c.unavailable.join(', ')}</div>}
    </div>
  )
  return (
    <Tooltip title={tip}>
      <Tag color={levelColor[c.level]} bordered={false} style={{ marginInlineEnd: 0 }}>
        {c.product} {c.version || '?'} · {t(levelText[c.level] ?? 'Unknown version')}
      </Tag>
    </Tooltip>
  )
}

/**
 * Confirms a destructive operation. On production instances the user must type
 * the resource name, like deleting a GitHub repository.
 */
export function useDangerConfirm(inst: Instance) {
  const { modal } = App.useApp()
  const t = useT()
  return (title: string, resource: string, onOk: () => Promise<unknown>) => {
    const prod = isProd(inst.env)
    let typed = ''
    modal.confirm({
      title,
      okType: 'danger',
      okText: t('Confirm'),
      cancelText: t('Cancel'),
      width: 480,
      content: (
        <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
          <Space size={6}>
            <span className="muted">{t('Instance')}</span> <b>{inst.name}</b> <EnvTag env={inst.env} />
          </Space>
          <Space size={6}>
            <span className="muted">{t('Target')}</span> <Typography.Text code>{resource}</Typography.Text>
          </Space>
          {prod && (
            <>
              <Alert type="error" showIcon message={t('This is production. Type the name to confirm.')} style={{ marginTop: 6 }} />
              <Input placeholder={resource} onChange={(e) => (typed = e.target.value)} />
            </>
          )}
        </Space>
      ),
      onOk: () => (prod && typed !== resource ? Promise.reject(new Error('name mismatch')) : onOk()),
    })
  }
}

export function CopyCommand({ cmd, title }: { cmd: string; title?: string }) {
  const t = useT()
  const { message } = App.useApp()
  return (
    <div className="code-block" style={{ marginTop: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 6 }}>
        <span className="muted" style={{ fontSize: 12 }}>{title ?? t('Equivalent CLI command')}</span>
        <CopyOutlined
          style={{ marginLeft: 'auto', cursor: 'pointer', color: 'var(--md-text-3)' }}
          onClick={() => navigator.clipboard.writeText(cmd).then(() => message.success(t('Copied')))}
        />
      </div>
      <pre className="mono">{cmd}</pre>
    </div>
  )
}

export interface TileProps {
  label: string
  value: ReactNode
  note?: ReactNode
  icon?: ReactNode
}

/** Stat tiles: values in primary ink; status is carried by an icon + label, never color alone. */
export function Tiles({ items }: { items: TileProps[] }) {
  return (
    <div className="tiles">
      {items.map((it) => (
        <div className="tile" key={it.label}>
          <div className="tile-label">{it.icon}{it.label}</div>
          <div className="tile-value">{it.value}</div>
          {it.note && <div className="tile-note">{it.note}</div>}
        </div>
      ))}
    </div>
  )
}

export const num = (n: number | undefined) => (n === undefined || Number.isNaN(n) ? '-' : n.toLocaleString())

export const fmtTime = (ms: number) => (ms ? dayjs(ms).format('YYYY-MM-DD HH:mm:ss') : '-')

export const fmtBytes = (b: number) => {
  if (!b) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(b) / Math.log(1024)), u.length - 1)
  return `${(b / 1024 ** i).toFixed(i ? 1 : 0)} ${u[i]}`
}

/**
 * Re-indents JSON text without parsing it, so 64-bit IDs and long decimals keep
 * every digit (JSON.parse would round them to doubles).
 */
export const prettyJSON = (s: string) => {
  const t = s.trim()
  if (!(t.startsWith('{') || t.startsWith('['))) return s
  try {
    JSON.parse(t) // validate only
  } catch {
    return s
  }
  let out = ''
  let depth = 0
  let inStr = false
  const nl = () => '\n' + '  '.repeat(depth)
  for (let i = 0; i < t.length; i++) {
    const c = t[i]
    if (inStr) {
      out += c
      if (c === '\\') out += t[++i]
      else if (c === '"') inStr = false
      continue
    }
    switch (c) {
      case '"':
        inStr = true
        out += c
        break
      case '{':
      case '[': {
        // Keep empty containers on one line.
        let j = i + 1
        while (/\s/.test(t[j])) j++
        if (t[j] === (c === '{' ? '}' : ']')) {
          out += c + t[j]
          i = j
        } else {
          depth++
          out += c + nl()
        }
        break
      }
      case '}':
      case ']':
        depth--
        out += nl() + c
        break
      case ',':
        out += ',' + nl()
        break
      case ':':
        out += ': '
        break
      default:
        if (!/\s/.test(c)) out += c
    }
  }
  return out
}
