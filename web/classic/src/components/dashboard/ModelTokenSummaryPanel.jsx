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

import React, { useMemo } from 'react';
import { Card, Button } from '@douyinfe/semi-ui';
import { Layers, Download } from 'lucide-react';
import { VChart } from '@visactor/react-vchart';
import { renderNumber } from '../../helpers';

const escapeCsvCell = (raw) => {
  const s = String(raw == null ? '' : raw);
  if (/[",\n\r]/.test(s)) {
    return '"' + s.replace(/"/g, '""') + '"';
  }
  return s;
};

const buildCsv = (rows, headers) => {
  const headerLine = headers.map((h) => escapeCsvCell(h.label)).join(',');
  const lines = rows.map((row) =>
    headers.map((h) => escapeCsvCell(row[h.key])).join(','),
  );
  // Prepend BOM so Excel recognizes UTF-8 (Chinese-friendly)
  return '﻿' + [headerLine, ...lines].join('\r\n');
};

const triggerDownload = (filename, content, mime) => {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};

const ModelTokenSummaryPanel = ({
  spec,
  summary,
  CARD_PROPS,
  CHART_CONFIG,
  FLEX_CENTER_GAP2,
  t,
}) => {
  const totalLine = useMemo(() => {
    const fmt = (n) => renderNumber(n || 0);
    const parts = [
      `${t('输入')} ${fmt(summary.totalPromptTokens)}`,
      `${t('输出')} ${fmt(summary.totalCompletionTokens)}`,
    ];
    if (summary.hasUnknown) {
      parts.push(`${t('未分类')} ${fmt(summary.totalUnknownTokens)}`);
    }
    return `${t('总计')}：${fmt(summary.totalTokens)} (${parts.join(' / ')})`;
  }, [
    summary.totalPromptTokens,
    summary.totalCompletionTokens,
    summary.totalUnknownTokens,
    summary.totalTokens,
    summary.hasUnknown,
    t,
  ]);

  const handleExportCsv = () => {
    const headers = [
      { key: 'model', label: t('模型') },
      { key: 'promptTokens', label: t('输入Token数') },
      { key: 'completionTokens', label: t('输出Token数') },
    ];
    if (summary.hasUnknown) {
      headers.push({ key: 'unknownTokens', label: t('未分类Token数') });
    }
    headers.push(
      { key: 'totalTokens', label: t('总Token数') },
      { key: 'count', label: t('调用次数') },
    );
    const csv = buildCsv(summary.rows || [], headers);
    triggerDownload(
      'model-token-summary.csv',
      csv,
      'text/csv;charset=utf-8',
    );
  };

  const hasData = (summary.rows || []).length > 0;

  return (
    <Card
      {...CARD_PROPS}
      className='!rounded-2xl'
      title={
        <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between w-full gap-3'>
          <div className={FLEX_CENTER_GAP2}>
            <Layers size={16} />
            {t('模型Token用量汇总')}
            <span className='text-gray-400 text-xs'>{totalLine}</span>
          </div>
          <Button
            icon={<Download size={14} />}
            size='small'
            type='tertiary'
            theme='light'
            disabled={!hasData}
            onClick={handleExportCsv}
          >
            {t('导出CSV')}
          </Button>
        </div>
      }
      bodyStyle={{ padding: 0 }}
    >
      <div className='h-96 p-2'>
        {hasData ? (
          <VChart spec={spec} option={CHART_CONFIG} />
        ) : (
          <div className='flex h-full items-center justify-center text-sm text-gray-400'>
            {t('暂无数据')}
          </div>
        )}
      </div>
    </Card>
  );
};

export default ModelTokenSummaryPanel;
