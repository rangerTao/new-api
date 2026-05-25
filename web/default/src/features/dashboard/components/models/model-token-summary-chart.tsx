/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { VChart } from '@visactor/react-vchart'
import { Download, Layers } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { VCHART_OPTION } from '@/lib/vchart'
import { Button } from '@/components/ui/button'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { processModelTokenSummary } from '@/features/dashboard/lib'
import type {
  ModelTokenSummaryRow,
  QuotaDataItem,
} from '@/features/dashboard/types'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

interface ModelTokenSummaryChartProps {
  data: QuotaDataItem[]
  loading?: boolean
  startTimestamp?: Date
  endTimestamp?: Date
}

function escapeCsvCell(raw: string | number): string {
  const s = String(raw ?? '')
  if (/[",\n\r]/.test(s)) {
    return '"' + s.replace(/"/g, '""') + '"'
  }
  return s
}

function buildCsv(
  rows: ModelTokenSummaryRow[],
  headers: { key: keyof ModelTokenSummaryRow; label: string }[]
): string {
  const headerLine = headers.map((h) => escapeCsvCell(h.label)).join(',')
  const lines = rows.map((row) =>
    headers.map((h) => escapeCsvCell(row[h.key] as string | number)).join(',')
  )
  // Prepend BOM so Excel recognizes UTF-8 and renders non-ASCII (e.g. Chinese) correctly
  return '﻿' + [headerLine, ...lines].join('\r\n')
}

function formatTimestampForFile(d?: Date): string {
  if (!d) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}`
}

function triggerDownload(filename: string, content: string, mime: string) {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

export function ModelTokenSummaryChart(props: ModelTokenSummaryChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  const summary = useMemo(
    () =>
      processModelTokenSummary(
        props.loading ? [] : props.data,
        t,
        customization.preset
      ),
    [props.data, props.loading, t, customization.preset]
  )

  const totalLine = useMemo(() => {
    const fmt = (n: number) =>
      Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(n)
    const parts = [
      `${t('Input')} ${fmt(summary.totalPromptTokens)}`,
      `${t('Output')} ${fmt(summary.totalCompletionTokens)}`,
    ]
    if (summary.hasUnknown) {
      parts.push(`${t('Unknown')} ${fmt(summary.totalUnknownTokens)}`)
    }
    return `${t('Total:')} ${fmt(summary.totalTokens)} (${parts.join(' / ')})`
  }, [
    summary.totalTokens,
    summary.totalPromptTokens,
    summary.totalCompletionTokens,
    summary.totalUnknownTokens,
    summary.hasUnknown,
    t,
  ])

  const handleExportCsv = () => {
    const headers: { key: keyof ModelTokenSummaryRow; label: string }[] = [
      { key: 'model', label: t('Model') },
      { key: 'promptTokens', label: t('Input Tokens') },
      { key: 'completionTokens', label: t('Output Tokens') },
    ]
    if (summary.hasUnknown) {
      headers.push({ key: 'unknownTokens', label: t('Unknown Tokens') })
    }
    headers.push(
      { key: 'totalTokens', label: t('Total Tokens') },
      { key: 'count', label: t('Call Count') }
    )
    const csv = buildCsv(summary.rows, headers)
    const start = formatTimestampForFile(props.startTimestamp)
    const end = formatTimestampForFile(props.endTimestamp)
    const range = start && end ? `_${start}_${end}` : ''
    triggerDownload(
      `model-token-summary${range}.csv`,
      csv,
      'text/csv;charset=utf-8'
    )
  }

  const hasData = summary.rows.length > 0

  const chartKey = [
    props.loading ? 'loading' : 'ready',
    summary.rows.length,
    resolvedTheme,
    customization.preset,
  ].join('-')

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex w-full flex-col gap-1.5 border-b px-3 py-2 sm:gap-3 sm:px-5 sm:py-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex items-center gap-2'>
          <Layers className='text-muted-foreground/60 size-4' />
          <div className='text-sm font-semibold'>
            {t('Model Token Usage Summary')}
          </div>
          <span className='text-muted-foreground text-xs'>{totalLine}</span>
        </div>

        <Button
          variant='outline'
          size='sm'
          onClick={handleExportCsv}
          disabled={!hasData || props.loading}
        >
          <Download className='mr-2 h-4 w-4' />
          {t('Export CSV')}
        </Button>
      </div>

      <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
        {themeReady && hasData && (
          <VChart
            key={chartKey}
            spec={{
              ...summary.spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
        {themeReady && !hasData && (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No data available')}
          </div>
        )}
      </div>
    </div>
  )
}
