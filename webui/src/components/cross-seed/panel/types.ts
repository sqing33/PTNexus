export interface TorrentSiteDetails {
  comment?: string
  torrentId?: string
  [key: string]: unknown
}

export interface WorkingTorrent {
  name: string
  save_path: string
  size: number
  size_formatted: string
  progress: number
  state: string
  sites: Record<string, TorrentSiteDetails>
  total_uploaded: number
  total_uploaded_formatted: string
  downloaderId?: string
  downloaderIds?: string[]
}

export interface DownloaderListItem {
  id: string
  name: string
}

