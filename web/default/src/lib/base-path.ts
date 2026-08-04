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

function normalizeBasePath(value: string | undefined): string {
  const trimmed = value?.trim()
  if (!trimmed || trimmed === '/') return ''
  const withLeadingSlash = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  return withLeadingSlash.replace(/\/+$/, '')
}

export const appBasePath = normalizeBasePath(
  import.meta.env.VITE_REACT_APP_BASE_PATH
)

export function withAppBasePath(path: string): string {
  if (!appBasePath || /^(?:[a-z][a-z\d+\-.]*:)?\/\//i.test(path)) {
    return path
  }
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `${appBasePath}${normalizedPath}`
}

export function getAppUrl(path = '/'): string {
  const appPath = withAppBasePath(path)
  if (typeof window === 'undefined') return appPath
  return `${window.location.origin}${appPath}`
}
