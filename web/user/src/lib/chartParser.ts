export interface ChartSpec {
  type: string
  title: string
  xAxis: { label: string; dataKey: string }
  series: { dataKey: string; name: string; color: string }[]
  data: Record<string, string | number>[]
}

export type ContentSegment =
  | { type: 'text'; content: string }
  | { type: 'chart'; spec: ChartSpec }

const chartBlockRe = /```chart\n([\s\S]*?)```/g

export function parseChartBlocks(content: string): ContentSegment[] {
  if (!content || typeof content !== 'string') {
    return [{ type: 'text', content: String(content ?? '') }]
  }
  const segments: ContentSegment[] = []
  let lastIndex = 0

  for (const match of content.matchAll(chartBlockRe)) {
    const matchStart = match.index!
    const matchEnd = matchStart + match[0].length

    if (matchStart > lastIndex) {
      segments.push({ type: 'text', content: content.slice(lastIndex, matchStart) })
    }

    try {
      const spec = JSON.parse(match[1].trim()) as ChartSpec
      if (spec.data?.length && spec.series?.length) {
        segments.push({ type: 'chart', spec })
      } else {
        segments.push({ type: 'text', content: match[0] })
      }
    } catch {
      segments.push({ type: 'text', content: match[0] })
    }

    lastIndex = matchEnd
  }

  if (lastIndex < content.length) {
    segments.push({ type: 'text', content: content.slice(lastIndex) })
  }

  return segments.length > 0 ? segments : [{ type: 'text', content }]
}
