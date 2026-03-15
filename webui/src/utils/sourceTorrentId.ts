export interface SourceTorrentSiteDetails {
  comment?: string | null
  torrentId?: string | null
}

const DIGITS_RE = /^\d+$/
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const INFOHASH_RE = /^[0-9a-f]{40}$/i
const RELATIVE_URL_BASE = 'https://pt-nexus.local'

const normalizeText = (value: string | null | undefined): string => {
  return typeof value === 'string' ? value.trim() : ''
}

const extractNumericQueryParam = (value: string, key: 'id' | 'torrent_id'): string | null => {
  const trimmed = normalizeText(value)
  if (!trimmed) return null

  try {
    const parsed = new URL(trimmed, RELATIVE_URL_BASE)
    const queryValue = normalizeText(parsed.searchParams.get(key))
    if (DIGITS_RE.test(queryValue)) {
      return queryValue
    }
  } catch {
    // Ignore URL parse errors and fall back to regex-based extraction.
  }

  const pattern = new RegExp(`${key}=([0-9]+)`, 'i')
  const matched = trimmed.match(pattern)
  return matched?.[1] || null
}

const extractUuidFromPath = (value: string): string | null => {
  const trimmed = normalizeText(value)
  if (!trimmed) return null

  if (UUID_RE.test(trimmed)) {
    return trimmed
  }

  try {
    const parsed = new URL(trimmed, RELATIVE_URL_BASE)
    const matchedPath = parsed.pathname.match(
      /\/torrent\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:\/|$)/i,
    )
    if (matchedPath?.[1]) {
      return matchedPath[1]
    }
  } catch {
    // Ignore URL parse errors and fall back to regex-based extraction.
  }

  const matched = trimmed.match(
    /\/torrent\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:\/|$)/i,
  )
  return matched?.[1] || null
}

const isUsableStoredSourceTorrentId = (value: string): boolean => {
  return DIGITS_RE.test(value) || UUID_RE.test(value)
}

export const extractSourceTorrentIdFromComment = (
  comment: string | null | undefined,
): string | null => {
  const trimmed = normalizeText(comment)
  if (!trimmed) {
    return null
  }

  if (DIGITS_RE.test(trimmed)) {
    return trimmed
  }

  const id = extractNumericQueryParam(trimmed, 'id')
  if (id) {
    return id
  }

  const torrentId = extractNumericQueryParam(trimmed, 'torrent_id')
  if (torrentId) {
    return torrentId
  }

  const uuid = extractUuidFromPath(trimmed)
  if (uuid) {
    return uuid
  }

  return null
}

export const resolveSourceTorrentId = ({
  sourceInfoTorrentId,
  siteDetails,
}: {
  sourceInfoTorrentId?: string | null
  siteDetails?: SourceTorrentSiteDetails | null
}): string | null => {
  const explicitTorrentId = normalizeText(sourceInfoTorrentId)
  if (explicitTorrentId) {
    return explicitTorrentId
  }

  const storedTorrentId = normalizeText(siteDetails?.torrentId)
  if (storedTorrentId && !INFOHASH_RE.test(storedTorrentId) && isUsableStoredSourceTorrentId(storedTorrentId)) {
    return storedTorrentId
  }

  return extractSourceTorrentIdFromComment(siteDetails?.comment)
}
