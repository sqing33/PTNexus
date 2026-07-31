export type MediaInfoSummaryField = {
  label: string
  value: string
}

export type MediaInfoSummary = {
  fileName: string
  general: MediaInfoSummaryField[]
  video: MediaInfoSummaryField[]
  audio: string[]
  subtitles: string[]
  hasSummary: boolean
}

type ParsedMediaInfoSection = {
  kind: string
  title: string
  fields: MediaInfoSummaryField[]
}

const sectionHeaderPattern = /^[A-Za-z][A-Za-z0-9 ()/#&.+-]*$/

const normalizeLabel = (label: string): string => label.replace(/\s+/g, ' ').trim()

const normalizeValue = (value: string): string => value.replace(/\s+/g, ' ').trim()

const normalizeMediaInfoText = (text: string): string =>
  (text || '').replace(/\r\n?/g, '\n').replace(/\u00a0/g, ' ')

const isSectionHeader = (line: string): boolean => {
  if (!line || line.includes(':')) return false
  if (!sectionHeaderPattern.test(line)) return false

  const kind = line.match(/^[A-Za-z]+/)?.[0].toLowerCase()
  return Boolean(
    kind && ['general', 'video', 'audio', 'text', 'subtitle', 'subtitles'].includes(kind),
  )
}

const getSectionKind = (title: string): string => title.match(/^[A-Za-z]+/)?.[0].toLowerCase() || ''

const parseSections = (text: string): ParsedMediaInfoSection[] => {
  const sections: ParsedMediaInfoSection[] = []
  let currentSection: ParsedMediaInfoSection | null = null

  for (const rawLine of normalizeMediaInfoText(text).split('\n')) {
    const line = rawLine.trim()
    if (!line) continue

    if (isSectionHeader(line)) {
      currentSection = {
        kind: getSectionKind(line),
        title: line,
        fields: [],
      }
      sections.push(currentSection)
      continue
    }

    const match = line.match(/^(.+?)\s*:\s*(.*)$/)
    if (!match || !currentSection) continue

    currentSection.fields.push({
      label: normalizeLabel(match[1]),
      value: normalizeValue(match[2]),
    })
  }

  return sections
}

const findFieldValue = (section: ParsedMediaInfoSection | undefined, labels: string[]): string => {
  if (!section) return ''
  const wanted = labels.map((label) => label.toLowerCase())
  return section.fields.find((field) => wanted.includes(field.label.toLowerCase()))?.value || ''
}

const buildField = (
  section: ParsedMediaInfoSection | undefined,
  label: string,
  labels: string[],
): MediaInfoSummaryField | null => {
  const value = findFieldValue(section, labels)
  return value ? { label, value } : null
}

const compactFields = (fields: Array<MediaInfoSummaryField | null>): MediaInfoSummaryField[] =>
  fields.filter((field): field is MediaInfoSummaryField => Boolean(field))

const getFileName = (value: string): string => {
  const trimmed = value.trim()
  if (!trimmed) return 'MediaInfo'
  return trimmed.split(/[\\/]/).pop() || trimmed
}

const buildVideoFields = (section: ParsedMediaInfoSection | undefined): MediaInfoSummaryField[] => {
  const format = findFieldValue(section, ['Format'])
  const bitDepth = findFieldValue(section, ['Bit depth'])
  const width = findFieldValue(section, ['Width'])
  const height = findFieldValue(section, ['Height'])
  const ratio = findFieldValue(section, ['Display aspect ratio'])
  const videoFormat = format ? `${format}${bitDepth ? ` (${bitDepth})` : ''}` : ''
  const resolution = width && height ? `${width} * ${height}${ratio ? ` (${ratio})` : ''}` : ''

  return compactFields([
    videoFormat ? { label: 'Format', value: videoFormat } : null,
    buildField(section, 'Bit Rate', ['Bit rate', 'Nominal bit rate']),
    resolution ? { label: 'Resolution', value: resolution } : null,
    buildField(section, 'Frame Rate', ['Frame rate']),
  ])
}

const buildAudioLine = (section: ParsedMediaInfoSection): string => {
  const title = findFieldValue(section, ['Title'])
  const language = findFieldValue(section, ['Language'])
  const format = findFieldValue(section, ['Format', 'Commercial name'])
  const channels = findFieldValue(section, ['Channel(s)'])
  const bitrate = findFieldValue(section, ['Bit rate'])
  const prefix = title || language
  const core = [prefix, format, channels].filter(Boolean).join(' ')
  const withBitrate = bitrate ? `${core} @ ${bitrate}` : core

  if (language && language !== prefix) return `${withBitrate} (${language})`
  return withBitrate || section.title
}

const buildSubtitleLine = (section: ParsedMediaInfoSection): string => {
  const title = findFieldValue(section, ['Title'])
  const language = findFieldValue(section, ['Language'])
  const format = findFieldValue(section, ['Format', 'Codec ID'])
  const base = [language, format].filter(Boolean).join(' ')
  const shouldShowTitle = title && title.toLowerCase() !== language.toLowerCase()

  if (base && shouldShowTitle) return `${base} (${title})`
  return base || title || section.title
}

export const buildMediaInfoSummary = (text: string): MediaInfoSummary => {
  const sections = parseSections(text)
  const generalSection = sections.find((section) => section.kind === 'general')
  const videoSection = sections.find((section) => section.kind === 'video')
  const audioSections = sections.filter((section) => section.kind === 'audio')
  const subtitleSections = sections.filter((section) =>
    ['text', 'subtitle', 'subtitles'].includes(section.kind),
  )

  const completeName = findFieldValue(generalSection, ['Complete name'])
  const general = compactFields([
    buildField(generalSection, 'Format', ['Format']),
    buildField(generalSection, 'Duration', ['Duration']),
    buildField(generalSection, 'File Size', ['File size']),
    buildField(generalSection, 'Bit Rate', ['Overall bit rate', 'Bit rate']),
  ])
  const video = buildVideoFields(videoSection)
  const audio = audioSections.map(buildAudioLine).filter(Boolean)
  const subtitles = subtitleSections.map(buildSubtitleLine).filter(Boolean)
  const hasSummary =
    general.length > 0 || video.length > 0 || audio.length > 0 || subtitles.length > 0

  return {
    fileName: getFileName(completeName),
    general,
    video,
    audio,
    subtitles,
    hasSummary,
  }
}
