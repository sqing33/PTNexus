export interface SiteData {
  uploaded: number
  comment: string
  migration: number
  state: string
  seeders: number
  torrentId?: string
  site?: string
  site_name?: string
}

export interface Torrent {
  unique_id: string
  hash?: string
  hashes?: string[]
  name: string
  save_path: string
  size: number
  size_formatted: string
  progress: number
  state: string
  sites: Record<string, SiteData>
  total_uploaded: number
  total_uploaded_formatted: string
  seeders: number
  downloaderId?: string
  downloaderIds?: string[]
  target_sites_count?: number
  publish_at?: string | null
  source_data_status?: 'missing' | 'unreviewed' | 'reviewed'
  source_data_fetched?: boolean
  source_data_reviewed?: boolean
}

export interface ISourceInfo {
  name: string
  site: string
  torrentId: string
}

export interface Downloader {
  id: string
  name: string
  enabled: boolean
  color?: string
}
