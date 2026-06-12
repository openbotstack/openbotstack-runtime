import { useMemo } from 'react'
import Markdown from 'react-markdown'
import { VitalChart } from './VitalChart'
import { parseChartBlocks } from '../lib/chartParser'

interface MessageContentProps {
  content: string
  streaming: boolean
}

export function MessageContent({ content, streaming }: MessageContentProps) {
  // Always render through markdown so streaming and final states share the same
  // layout (line-height, paragraph spacing) — avoids the "wide jump" when
  // switching from plain text to markdown at stream end, and shows structured
  // content (headings/lists/code) live during streaming instead of raw chars.
  const segments = useMemo(() => parseChartBlocks(content), [content])

  return (
    <div className="content markdown-body">
      {segments.map((segment, i) => {
        if (segment.type === 'chart') {
          return <VitalChart key={i} spec={segment.spec} />
        }
        return <Markdown key={i}>{segment.content}</Markdown>
      })}
      {streaming && <span className="streaming-cursor" />}
    </div>
  )
}
