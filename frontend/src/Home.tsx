import { useMemo } from 'react'
import { ApiOutlined, BookOutlined, ImportOutlined, PlusOutlined } from '@ant-design/icons'
import { EnvTag, isProd, KindIcon, Tiles } from './common'
import { useT } from './settings'
import { kindLabel } from './theme'
import type { Instance } from './types'

interface Props {
  instances: Instance[]
  connected: Set<string>
  onOpen: (id: string) => void
  onNew: () => void
  onImport: () => void
}

export default function Home({ instances, connected, onOpen, onNew, onImport }: Props) {
  const t = useT()
  const count = (k: string) => instances.filter((i) => i.kind === k).length
  const sorted = useMemo(
    () => [...instances].sort((a, b) => Number(connected.has(b.id)) - Number(connected.has(a.id)) || a.name.localeCompare(b.name)),
    [instances, connected],
  )

  return (
    <div className="home">
      <div className="drag" style={{ position: 'fixed', top: 0, left: 268, right: 0, height: 32 }} />
      <h1>{t('One deck for all your middleware')}</h1>
      <div className="home-sub">{t('Manage Kafka, Redis and RabbitMQ, even behind bastion hosts.')}</div>

      {instances.length > 0 && (
        <Tiles
          items={[
            { label: t('Connections'), value: instances.length, note: t('{n} connected', { n: connected.size }) },
            { label: t('Production'), value: instances.filter((i) => isProd(i.env)).length, note: t('Destructive actions need typed confirmation') },
            { label: 'Kafka', value: count('kafka'), icon: <KindIcon kind="kafka" size={14} /> },
            { label: 'Redis', value: count('redis'), icon: <KindIcon kind="redis" size={14} /> },
            { label: 'RabbitMQ', value: count('rabbitmq'), icon: <KindIcon kind="rabbitmq" size={14} /> },
          ]}
        />
      )}

      <div className="home-actions">
        <div className="action-card" onClick={onNew}>
          <PlusOutlined style={{ fontSize: 18 }} />
          <div className="title">{t('New connection')}</div>
          <div className="desc">{t('Direct, through SSH jump hosts, or a SOCKS5 proxy.')}</div>
        </div>
        <div className="action-card" onClick={onImport}>
          <ImportOutlined style={{ fontSize: 18 }} />
          <div className="title">{t('Import from a teammate')}</div>
          <div className="desc">{t('Load a bundle exported from MidDeck, optionally encrypted.')}</div>
        </div>
        <a className="action-card" href="https://github.com/YunMuZhe/MidDeck#readme" target="_blank" rel="noreferrer" style={{ color: 'inherit' }}>
          <BookOutlined style={{ fontSize: 18 }} />
          <div className="title">{t('Documentation')}</div>
          <div className="desc">{t('Install guide, version support and roadmap.')}</div>
        </a>
      </div>

      {sorted.length > 0 ? (
        <>
          <div className="section-title">{t('All connections')}</div>
          <div className="inst-grid">
            {sorted.map((i) => (
              <div className="inst-card" key={i.id} onClick={() => onOpen(i.id)}>
                <KindIcon kind={i.kind} size={32} />
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <span className="n">{i.name}</span>
                    {connected.has(i.id) && <span className="live-dot" />}
                  </div>
                  <div className="a mono">{(i.addrs ?? [])[0] ?? i.mgmtUrl}</div>
                  <div style={{ display: 'flex', gap: 6, marginTop: 6, alignItems: 'center', minWidth: 0 }}>
                    <EnvTag env={i.env} />
                    <span className="muted cell-ellipsis" style={{ fontSize: 11.5, minWidth: 0, flex: 1 }}>{kindLabel[i.kind]}{i.project ? ` · ${i.project}` : ''}</span>
                    {(i.network?.ssh?.length ?? 0) > 0 && <span className="muted" style={{ fontSize: 11.5, whiteSpace: 'nowrap' }}><ApiOutlined /> SSH</span>}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </>
      ) : null}
    </div>
  )
}
