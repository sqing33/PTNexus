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

const normalizeBitrate = (value: string): string =>
  value
    .replace(/(\d)\s+(?=\d)/g, '$1')
    .replace(/\s+(?=[kKmMgG]b\/s)/g, '')
    .replace(/([kKmMgG])b\/s/g, (_, unit: string) => {
      return `${unit.toLowerCase()}b/s`
    })

const normalizePixels = (value: string): string =>
  value
    .replace(/\s+/g, '')
    .replace(/pixels?/gi, '')
    .trim()

const normalizeAudioCodec = (format: string, commercialName: string): string => {
  const source = commercialName || format
  if (/truehd/i.test(source) && /atmos/i.test(source)) return 'TrueHD Atmos'
  if (/truehd/i.test(source)) return 'TrueHD'
  if (/dts-hd\s*master\s*audio/i.test(source) || /dts-hd\s*ma/i.test(source)) return 'DTS-HD MA'
  if (/dts\s*xll/i.test(format)) return 'DTS-HD MA'
  if (/dts:x/i.test(source)) return 'DTS:X'
  if (/dolby\s*digital\s*plus/i.test(source)) return 'DD+'
  if (/dolby\s*digital/i.test(source)) return format || 'AC-3'
  return format || source
}

const normalizeAudioChannels = (value: string): string => {
  const match = value.match(/(\d+(?:\.\d+)?)/)
  if (!match) return value

  const channels = Number(match[1])
  if (!Number.isFinite(channels)) return value
  if (channels === 1) return '1.0ch'
  if (channels === 2) return '2.0ch'
  if (channels === 6) return '5.1ch'
  if (channels === 8) return '7.1ch'
  return `${channels}ch`
}

const buildVideoFields = (section: ParsedMediaInfoSection | undefined): MediaInfoSummaryField[] => {
  const format = findFieldValue(section, ['Format'])
  const bitDepth = findFieldValue(section, ['Bit depth'])
  const width = findFieldValue(section, ['Width'])
  const height = findFieldValue(section, ['Height'])
  const ratio = findFieldValue(section, ['Display aspect ratio'])
  const videoFormat = format ? `${format}${bitDepth ? ` (${bitDepth})` : ''}` : ''
  const bitrate = findFieldValue(section, ['Bit rate', 'Nominal bit rate'])
  const frameRate = findFieldValue(section, ['Frame rate'])
  const hdrFormat = findFieldValue(section, ['HDR format'])
  const resolution =
    width && height
      ? `${normalizePixels(width)}*${normalizePixels(height)}${ratio ? ` (${ratio})` : ''}`
      : ''
  const hdrFields = hdrFormat
    .split(/\s+\/\s+/)
    .map((value) => value.trim())
    .filter(Boolean)
    .map((value) => ({ label: 'HDR', value }))

  return [
    videoFormat ? { label: 'Format', value: videoFormat } : null,
    bitrate ? { label: 'Bit Rate', value: normalizeBitrate(bitrate) } : null,
    resolution ? { label: 'Resolution', value: resolution } : null,
    frameRate ? { label: 'Frame Rate', value: frameRate } : null,
  ]
    .concat(hdrFields)
    .filter((field): field is MediaInfoSummaryField => Boolean(field))
}

const buildAudioLine = (section: ParsedMediaInfoSection): string => {
  const title = findFieldValue(section, ['Title'])
  const language = findFieldValue(section, ['Language'])
  const format = findFieldValue(section, ['Format'])
  const commercialName = findFieldValue(section, ['Commercial name'])
  const channels = findFieldValue(section, ['Channel(s)'])
  const bitrate = findFieldValue(section, ['Bit rate'])
  const prefix = language || title
  const codec = normalizeAudioCodec(format, commercialName)
  const suffix = title ? ` (${title})` : ''
  const core = [prefix, codec, channels ? normalizeAudioChannels(channels) : '']
    .filter(Boolean)
    .join(' ')
  return (
    `${core}${bitrate ? ` @ ${normalizeBitrate(bitrate)}` : ''}${suffix}`.trim() || section.title
  )
}

const buildSubtitleLine = (section: ParsedMediaInfoSection): string => {
  const title = findFieldValue(section, ['Title'])
  const language = findFieldValue(section, ['Language'])
  const format = findFieldValue(section, ['Format', 'Codec ID'])
  const suffix = title && title !== language ? ` (${title})` : ''
  const base = [language, format].filter(Boolean).join(' ')
  return `${base}${suffix}`.trim() || title || section.title
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
  const generalBitrate = findFieldValue(generalSection, ['Overall bit rate', 'Bit rate'])
  const general = compactFields([
    buildField(generalSection, 'Format', ['Format']),
    buildField(generalSection, 'Duration', ['Duration']),
    buildField(generalSection, 'File Size', ['File size']),
    generalBitrate ? { label: 'Bit Rate', value: normalizeBitrate(generalBitrate) } : null,
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
