import { Tabs } from 'antd'
import type { Instance } from '../types'
import TopicsTab from './TopicsTab'
import MessagesTab from './MessagesTab'
import GroupsTab from './GroupsTab'
import { useState } from 'react'

export default function KafkaView({ inst }: { inst: Instance }) {
  const [tab, setTab] = useState('topics')
  const [msgTopic, setMsgTopic] = useState<string>()
  return (
    <Tabs
      activeKey={tab}
      onChange={setTab}
      items={[
        {
          key: 'topics',
          label: 'Topic',
          children: <TopicsTab inst={inst} onBrowse={(t) => { setMsgTopic(t); setTab('messages') }} />,
        },
        { key: 'messages', label: '消息', children: <MessagesTab inst={inst} topic={msgTopic} /> },
        { key: 'groups', label: '消费组', children: <GroupsTab inst={inst} /> },
      ]}
    />
  )
}

/** Equivalent CLI commands, shown next to forms so users can copy them to a server. */
export const kafkaCli = {
  bootstrap: (inst: Instance) => (inst.addrs ?? []).join(','),
  createTopic: (inst: Instance, name: string, p: number, r: number, cfg: Record<string, string>) =>
    `kafka-topics.sh --bootstrap-server ${kafkaCli.bootstrap(inst)} --create --topic ${name || '<topic>'} --partitions ${p} --replication-factor ${r}` +
    Object.entries(cfg).map(([k, v]) => ` --config ${k}=${v}`).join(''),
  resetOffsets: (inst: Instance, group: string, topic: string, mode: string, value: number) => {
    const m: Record<string, string> = {
      earliest: '--to-earliest',
      latest: '--to-latest',
      offset: `--to-offset ${value}`,
      shift: `--shift-by ${value}`,
      timestamp: `--to-datetime ${new Date(value || Date.now()).toISOString().replace('Z', '')}`,
    }
    return `kafka-consumer-groups.sh --bootstrap-server ${kafkaCli.bootstrap(inst)} --group ${group} --topic ${topic} --reset-offsets ${m[mode] ?? ''} --execute`
  },
}
