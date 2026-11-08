import { useCallback, useEffect, useMemo, useState } from 'react'
import { App as AntApp, Button, Dropdown, Empty, Input, Layout, Space, Tooltip, Tree, Typography } from 'antd'
import type { DataNode } from 'antd/es/tree'
import {
  AuditOutlined, DatabaseOutlined, ExportOutlined, ImportOutlined, InfoCircleOutlined, PlusOutlined, ReloadOutlined,
} from '@ant-design/icons'
import { api, errMsg } from './api'
import { EnvTag, kindColor, kindLabel } from './common'
import InstanceForm from './InstanceForm'
import InstanceView from './InstanceView'
import { AboutModal, AuditModal, ExportModal, ImportModal } from './Dialogs'
import type { Instance } from './types'

const { Sider, Content } = Layout

function matches(i: Instance, q: string) {
  if (!q) return true
  const hay = [i.name, i.project, i.env, i.kind, i.description, ...(i.tags ?? []), ...(i.aliases ?? []), ...(i.addrs ?? [])]
    .join(' ')
    .toLowerCase()
  return q.toLowerCase().split(/\s+/).every((t) => hay.includes(t))
}

export default function App() {
  const { message } = AntApp.useApp()
  const [instances, setInstances] = useState<Instance[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [editing, setEditing] = useState<Instance | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [dialog, setDialog] = useState<'import' | 'export' | 'audit' | 'about' | null>(null)

  const reload = useCallback(async () => {
    try {
      setInstances(await api.listInstances())
    } catch (e) {
      message.error(errMsg(e))
    }
  }, [message])

  useEffect(() => {
    reload()
  }, [reload])

  const projects = useMemo(() => Array.from(new Set(instances.map((i) => i.project).filter(Boolean))), [instances])

  const tree: DataNode[] = useMemo(() => {
    const groups = new Map<string, Map<string, Instance[]>>()
    for (const i of instances.filter((x) => matches(x, query))) {
      const p = i.project || '未分组'
      if (!groups.has(p)) groups.set(p, new Map())
      const envs = groups.get(p)!
      const e = i.env || 'default'
      if (!envs.has(e)) envs.set(e, [])
      envs.get(e)!.push(i)
    }
    return Array.from(groups.entries()).map(([p, envs]) => ({
      key: 'p:' + p,
      title: <Typography.Text strong>{p}</Typography.Text>,
      selectable: false,
      children: Array.from(envs.entries()).map(([e, list]) => ({
        key: `e:${p}:${e}`,
        title: <EnvTag env={e} />,
        selectable: false,
        children: list.map((i) => ({
          key: i.id,
          isLeaf: true,
          title: (
            <Space size={4}>
              <DatabaseOutlined style={{ color: `var(--ant-${kindColor[i.kind]}-6, #888)` }} />
              <span>{i.name}</span>
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>{kindLabel[i.kind]}</Typography.Text>
            </Space>
          ),
        })),
      })),
    }))
  }, [instances, query])

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

  return (
    <Layout style={{ height: '100vh' }}>
      <Sider width={300} theme="light" style={{ borderRight: '1px solid #f0f0f0' }}>
        <div style={{ padding: '14px 12px 8px' }}>
          <Space style={{ width: '100%', justifyContent: 'space-between', marginBottom: 10 }}>
            <Typography.Title level={4} style={{ margin: 0 }}>MidDeck</Typography.Title>
            <Space size={2}>
              <Tooltip title="新建连接">
                <Button type="primary" size="small" icon={<PlusOutlined />} onClick={() => openEditor(null)} />
              </Tooltip>
              <Dropdown
                menu={{
                  items: [
                    { key: 'import', icon: <ImportOutlined />, label: '导入配置' },
                    { key: 'export', icon: <ExportOutlined />, label: '导出配置' },
                    { key: 'audit', icon: <AuditOutlined />, label: '操作审计' },
                    { key: 'about', icon: <InfoCircleOutlined />, label: '版本支持 / 关于' },
                  ],
                  onClick: ({ key }) => setDialog(key as typeof dialog),
                }}
              >
                <Button size="small">更多</Button>
              </Dropdown>
              <Tooltip title="刷新">
                <Button size="small" type="text" icon={<ReloadOutlined />} onClick={reload} />
              </Tooltip>
            </Space>
          </Space>
          <Input.Search allowClear placeholder="搜索名称 / 别名 / 标签 / 地址" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        <div className="sider-scroll">
          {instances.length === 0 ? (
            <Empty style={{ marginTop: 60 }} description="还没有连接">
              <Space direction="vertical">
                <Button type="primary" onClick={() => openEditor(null)}>新建连接</Button>
                <Button onClick={() => setDialog('import')}>从同事导出的文件导入</Button>
              </Space>
            </Empty>
          ) : (
            <Tree
              blockNode
              defaultExpandAll
              key={query + instances.length}
              treeData={tree}
              selectedKeys={selected ? [selected] : []}
              onSelect={(keys) => keys[0] && setSelected(String(keys[0]))}
              style={{ padding: '0 4px' }}
            />
          )}
        </div>
      </Sider>
      <Content className="content-scroll" style={{ background: '#fff' }}>
        {current ? (
          <InstanceView
            key={current.id}
            inst={current}
            onEdit={() => openEditor(current)}
            onDeleted={() => {
              setSelected(null)
              reload()
            }}
          />
        ) : (
          <Empty style={{ marginTop: '25vh' }} description="从左侧选择一个中间件实例" />
        )}
      </Content>
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
      <AboutModal open={dialog === 'about'} onClose={() => setDialog(null)} />
    </Layout>
  )
}
