import React from 'react'
import ReactDOM from 'react-dom/client'
import { App as AntApp, ConfigProvider } from 'antd'
import enUS from 'antd/locale/en_US'
import zhCN from 'antd/locale/zh_CN'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import App from './App'
import { api } from './api'
import { SettingsProvider, useSettings, type Lang } from './settings'
import { buildTheme } from './theme'
import './style.css'

document.documentElement.dataset.platform = /Mac/.test(navigator.userAgent) ? 'mac' : 'other'

function Themed() {
  const { lang, dark } = useSettings()
  dayjs.locale(lang === 'zh' ? 'zh-cn' : 'en')
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  return (
    <ConfigProvider locale={lang === 'zh' ? zhCN : enUS} theme={buildTheme(dark)}>
      <AntApp>
        <App />
      </AntApp>
    </ConfigProvider>
  )
}

const syncLang = (l: Lang) => {
  api.setLanguage(l).catch(() => {})
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <SettingsProvider onLangChange={syncLang}>
      <Themed />
    </SettingsProvider>
  </React.StrictMode>,
)
