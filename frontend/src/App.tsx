import { useCallback, useEffect, useMemo, useState } from 'react'
import { App as AntApp, Button, Dropdown, Input, Segmented, Tooltip } from 'antd'
import {
  AuditOutlined, CaretDownOutlined, CaretRightOutlined, ExportOutlined, ImportOutlined, InfoCircleOutlined,
  PlusOutlined, SearchOutlined, SettingOutlined,
} from '@ant-design/icons'
import logo from './assets/logo.png'
import { api, errMsg } from './api'
import { EnvTag, KindIcon } from './common'
import InstanceForm from './InstanceForm'
import InstanceView from './InstanceView'
import Home from './Home'
import { AboutModal, AuditModal, ExportModal, ImportModal } from './Dialogs'
import { useSettings } from './settings'
import type { Instance } from './types'

const envOrder: Record<string, number> = { prod: 0, production: 0, staging: 1, test: 2, dev: 3 }

function matches(i: Instance, q: string) {
  if (!q) return true
  const hay = [i.name, i.project, i.env, i.kind, i.description, ...(i.tags ?? []), ...(i.aliases ?? []), ...(i.addrs ?? []), i.mgmtUrl]
    .join(' ')
    .toLowerCase()
  return q.toLowerCase().split(/\s+/).every((t) => hay.includes(t))
}

type DialogName = 'import' | 'export' | 'audit' | 'about' | null

export default function App() {
  const { message } = AntApp.useApp()
  const { t, lang, setLang, themeMode, setThemeMode } = useSettings()
  const [instances, setInstances] = useState<Instance[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [connected, setConnected] = useState<Set<string>>(new Set())
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const [query, setQuery] = useState('')
  const [editing, setEditing] = useState<Instance | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [dialog, setDialog] = useState<DialogName>(null)
  const [version, setVersion] = useState('')

  const reload = useCallback(async () => {
    try {
      setInstances(await api.listInstances())
    } catch (e) {
      message.error(errMsg(e))
    }
  }, [message])

  useEffect(() => {
    reload()
    api.version().then(setVersion).catch(() => {})
  }, [reload])

  const projects = useMemo(() => Array.from(new Set(instances.map((i) => i.project).filter(Boolean))), [instances])

  const groups = useMemo(() => {
    const m = new Map<string, Instance[]>()
    for (const i of instances.filter((x) => matches(x, query))) {
      const p = i.project || t('Ungrouped')
      if (!m.has(p)) m.set(p, [])
      m.get(p)!.push(i)
    }
    for (const list of m.values()) {
      list.sort((a, b) => (envOrder[a.env] ?? 9) - (envOrder[b.env] ?? 9) || a.name.localeCompare(b.name))
    }
    return Array.from(m.entries()).sort(([a], [b]) => a.localeCompare(b))
  }, [instances, query, t])

  const current = instances.find((i) => i.id === selected) ?? null

  const openEditor = async (inst: Instance | null) => {
    if (inst) {
      try {
        inst = await api.getInstance(inst.id)
      } catch (e) {
        message.error(errMsg(e))
        return
      }
    }
    setEditing(inst)
    setFormOpen(true)
  }

  const markConnected = useCallback((id: string, on: boolean) => {
    setConnected((s) => {
      if (s.has(id) === on) return s
      const n = new Set(s)
      if (on) n.add(id)
      else n.delete(id)
      return n
    })
  }, [])

  const toggleGroup = (p: string) =>
    setCollapsed((s) => {
      const n = new Set(s)
      if (n.has(p)) n.delete(p)
      else n.add(p)
      return n
    })

  const settingsMenu = (
    <div className="ant-dropdown-menu" style={{ padding: 12, width: 240 }}>
      <div className="muted" style={{ fontSize: 12, marginBottom: 6 }}>{t('Language')}</div>
      <Segmented block value={lang} onChange={(v) => setLang(v as 'en' | 'zh')} options={[{ value: 'en', label: 'English' }, { value: 'zh', label: '中文' }]} />
      <div className="muted" style={{ fontSize: 12, margin: '12px 0 6px' }}>{t('Appearance')}</div>
      <Segmented
        block
        value={themeMode}
        onChange={(v) => setThemeMode(v as 'light' | 'dark' | 'system')}
        options={[{ value: 'light', label: t('Light') }, { value: 'dark', label: t('Dark') }, { value: 'system', label: t('System') }]}
      />
    </div>
  )

  return (
    <div className="shell">
      <aside className="sider">
        <div className="sider-top drag">
          <div className="brand" onClick={() => setSelected(null)} style={{ cursor: 'pointer' }}>
            <img src={logo} alt="" />
            <span className="brand-name">MidDeck</span>
            <span className="brand-ver">{version}</span>
          </div>
          <div className="no-drag" style={{ display: 'flex', gap: 6 }}>
            <Input
              allowClear
              prefix={<SearchOutlined className="muted" />}
              placeholder={t('Search name, alias, tag, address')}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <Tooltip title={t('New connection')}>
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor(null)} />
            </Tooltip>
          </div>
        </div>
        <div className="sider-list">
          {groups.map(([p, list]) => (
            <div key={p}>
              <div className="group-head" onClick={() => toggleGroup(p)}>
                {collapsed.has(p) ? <CaretRightOutlined /> : <CaretDownOutlined />}
                <span>{p}</span>
                <span className="count">{list.length}</span>
              </div>
              {!collapsed.has(p) &&
                list.map((i) => (
                  <div key={i.id} className={'inst-item' + (i.id === selected ? ' active' : '')} onClick={() => setSelected(i.id)}>
                    <KindIcon kind={i.kind} size={20} />
                    <span className="name" title={i.name}>{i.name}</span>
                    {connected.has(i.id) && <Tooltip title={t('Connected')}><span className="live" /></Tooltip>}
                    <EnvTag env={i.env} />
                  </div>
                ))}
            </div>
          ))}
          {instances.length > 0 && groups.length === 0 && <div className="muted" style={{ padding: 16, textAlign: 'center' }}>{t('No matches')}</div>}
        </div>
        <div className="sider-foot">
          <Tooltip title={t('Import')}><Button type="text" icon={<ImportOutlined />} onClick={() => setDialog('import')} /></Tooltip>
          <Tooltip title={t('Export')}><Button type="text" icon={<ExportOutlined />} onClick={() => setDialog('export')} disabled={!instances.length} /></Tooltip>
          <Tooltip title={t('Audit log')}><Button type="text" icon={<AuditOutlined />} onClick={() => setDialog('audit')} /></Tooltip>
          <Tooltip title={t('Version support & about')}><Button type="text" icon={<InfoCircleOutlined />} onClick={() => setDialog('about')} /></Tooltip>
          <div style={{ flex: 1 }} />
          <Dropdown dropdownRender={() => settingsMenu} trigger={['click']} placement="topRight">
            <Button type="text" icon={<SettingOutlined />}>{lang === 'zh' ? '中文' : 'EN'}</Button>
          </Dropdown>
        </div>
      </aside>

      <main className="main">
        {current ? (
          <InstanceView
            key={current.id}
            inst={current}
            onEdit={() => openEditor(current)}
            onConnected={(on) => markConnected(current.id, on)}
            onDeleted={() => {
              setSelected(null)
              reload()
            }}
          />
        ) : (
          <Home
            instances={instances}
            connected={connected}
            onOpen={setSelected}
            onNew={() => openEditor(null)}
            onImport={() => setDialog('import')}
          />
        )}
      </main>

      <InstanceForm
        open={formOpen}
        initial={editing}
        projects={projects}
        onClose={() => setFormOpen(false)}
        onSaved={(i) => {
          setFormOpen(false)
          setSelected(i.id)
          reload()
        }}
      />
      <ImportModal open={dialog === 'import'} onClose={() => setDialog(null)} onDone={reload} />
      <ExportModal open={dialog === 'export'} instances={instances} onClose={() => setDialog(null)} />
      <AuditModal open={dialog === 'audit'} onClose={() => setDialog(null)} />
      <AboutModal open={dialog === 'about'} version={version} onClose={() => setDialog(null)} />
    </div>
  )
}
