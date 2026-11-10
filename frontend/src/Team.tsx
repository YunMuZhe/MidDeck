import { useEffect, useState } from 'react'
import { Alert, App, Button, Empty, Modal, Space, Table, Tag, Tooltip, Typography } from 'antd'
import { BranchesOutlined, FolderOpenOutlined, SyncOutlined } from '@ant-design/icons'
import { api, errMsg } from './api'
import { EnvTag, KindIcon } from './common'
import { useT } from './settings'
import type { CatalogEntry, Person, TeamStatus } from './types'

const who = (p?: Person) => (p ? p.name || p.email : '')

/** Team repository: a Git working copy that holds the shared connection catalog. */
export function TeamModal({ open, onClose, onChanged }: { open: boolean; onClose: () => void; onChanged: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [st, setSt] = useState<TeamStatus | null>(null)
  const [busy, setBusy] = useState('')
  const [tick, setTick] = useState(0)

  const run = async (name: string, fn: () => Promise<TeamStatus | void>, ok?: string) => {
    setBusy(name)
    try {
      const r = await fn()
      setSt(r ?? (await api.teamStatus()))
      setTick((x) => x + 1)
      onChanged()
      if (ok) message.success(ok)
    } catch (e) {
      message.error(errMsg(e))
      api.teamStatus().then(setSt).catch(() => {})
    } finally {
      setBusy('')
    }
  }

  useEffect(() => {
    if (!open) return
    api.teamStatus().then((s) => {
      setSt(s)
      // Opening the panel is a good moment to pick up what teammates pushed.
      if (s.configured && !s.error && s.upstream) run('sync', () => api.teamSync())
    }).catch((e) => message.error(errMsg(e)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const ready = !!st?.configured && !st.error

  return (
    <Modal title={t('Team')} open={open} onCancel={onClose} footer={null} width={980}>
      {!st?.configured ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={
            <div style={{ maxWidth: 520, margin: '0 auto' }}>
              <Typography.Paragraph>{t('Sync connections and jump hosts with your team through a Git repository.')}</Typography.Paragraph>
              <Typography.Paragraph type="secondary">{t('Pick a folder that is a Git working copy (clone your team repository first). Passwords never go into it.')}</Typography.Paragraph>
            </div>
          }
        >
          <Button type="primary" icon={<FolderOpenOutlined />} loading={busy === 'pick'} onClick={() => run('pick', () => api.teamPickRepo())}>{t('Choose repository folder')}</Button>
        </Empty>
      ) : (
        <>
          <div className="team-head">
            <Space size={8} wrap>
              <Typography.Text className="mono selectable" style={{ fontSize: 12 }}>{st.repo}</Typography.Text>
              {st.branch && <Tag bordered={false} icon={<BranchesOutlined />}>{st.branch}</Tag>}
              {ready && !st.upstream && <Tooltip title={t('This branch has no remote to push to; teammates will not see changes.')}><Tag color="warning">{t('no remote')}</Tag></Tooltip>}
              {st.ahead > 0 && <Tag color="blue">{t('{n} to push', { n: st.ahead })}</Tag>}
              {st.behind > 0 && <Tag color="gold">{t('{n} to pull', { n: st.behind })}</Tag>}
              {st.dirty > 0 && <Tooltip title={t('Files changed in the repository but not committed.')}><Tag>{t('{n} uncommitted', { n: st.dirty })}</Tag></Tooltip>}
            </Space>
            <div style={{ flex: 1 }} />
            <Space>
              <span className="muted" style={{ fontSize: 12 }}>{st.user.email ? `${who(st.user)} <${st.user.email}>` : t('git identity not set')}</span>
              <Button size="small" icon={<SyncOutlined />} loading={busy === 'sync'} disabled={!ready} onClick={() => run('sync', () => api.teamSync(), t('Synced'))}>{t('Sync')}</Button>
              <Button size="small" onClick={() => run('pick', () => api.teamPickRepo())}>{t('Change…')}</Button>
              <Button size="small" danger type="text" onClick={() => run('pick', () => api.teamSetRepo(''))}>{t('Disconnect')}</Button>
            </Space>
          </div>
          {st.error && <Alert type="error" showIcon message={st.error} style={{ marginBottom: 12 }} />}
          {ready && !st.user.email && <Alert type="warning" showIcon message={t('Set git user.name and user.email in this repository so your commits have an author.')} style={{ marginBottom: 12 }} />}
          {ready && (
            <Catalog tick={tick} onDone={(m) => run('', async () => {}, m)} />
          )}
        </>
      )}
    </Modal>
  )
}

const catalogTag: Record<CatalogEntry['status'], [string, string]> = {
  same: ['green', 'in sync'], changed: ['gold', 'differs'], teamOnly: ['blue', 'team only'], localOnly: ['default', 'not shared'],
}

function Catalog({ tick, onDone }: { tick: number; onDone: (msg?: string) => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [rows, setRows] = useState<CatalogEntry[]>([])
  const [sel, setSel] = useState<string[]>([])
  const [busy, setBusy] = useState('')
  useEffect(() => {
    api.teamCatalog().then((r) => {
      setRows(r)
      setSel(r.filter((x) => x.status === 'same' || x.status === 'changed').map((x) => x.id))
    }).catch((e) => message.error(errMsg(e)))
  }, [tick, message])

  const act = async (name: string, fn: () => Promise<string>) => {
    setBusy(name)
    try {
      onDone(await fn())
    } catch (e) {
      message.error(errMsg(e))
      onDone()
    } finally {
      setBusy('')
    }
  }
  const mine = sel.filter((id) => rows.find((r) => r.id === id)?.status !== 'teamOnly')
  const theirs = sel.filter((id) => ['teamOnly', 'changed'].includes(rows.find((r) => r.id === id)?.status ?? ''))
  return (
    <>
      <div className="catalog-bar">
        <span className="muted" style={{ fontSize: 12, flex: 1 }}>{t('The catalog holds addresses, jump hosts and settings, never passwords. Everyone fills in their own after importing.')}</span>
        <Space size={8} style={{ flex: 'none' }}>
        <Tooltip title={t('The team catalog will list exactly the selected connections of yours, next to the ones only teammates have.')}>
          <Button size="small" type="primary" loading={busy === 'pub'} onClick={() => act('pub', async () => { await api.teamPublishCatalog(mine); return t('Published {n} connection(s)', { n: mine.length }) })}>
            {t('Publish selected ({n})', { n: mine.length })}
          </Button>
        </Tooltip>
        <Tooltip title={t('Add or update these connections here. Passwords you already saved are kept.')}>
          <Button size="small" disabled={!theirs.length} loading={busy === 'imp'} onClick={() => act('imp', async () => { const r = await api.teamImportCatalog(theirs); return t('Added {a}, overwritten {o}, skipped {s}', { a: r.added, o: r.overwritten, s: r.skipped }) })}>
            {t('Import selected ({n})', { n: theirs.length })}
          </Button>
        </Tooltip>
        </Space>
      </div>
      <Table
        size="small"
        rowKey="id"
        dataSource={rows}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        rowSelection={{ selectedRowKeys: sel, onChange: (k) => setSel(k as string[]) }}
        columns={[
          { title: t('Name'), dataIndex: 'name', render: (n, r) => <Space size={6}><KindIcon kind={r.kind} size={16} />{n}</Space> },
          { title: t('Project'), dataIndex: 'project' },
          { title: t('Environment'), dataIndex: 'env', render: (e) => <EnvTag env={e} /> },
          { title: t('Status'), dataIndex: 'status', render: (s: CatalogEntry['status']) => <Tag bordered={false} color={catalogTag[s][0]}>{t(catalogTag[s][1])}</Tag> },
        ]}
      />
    </>
  )
}
