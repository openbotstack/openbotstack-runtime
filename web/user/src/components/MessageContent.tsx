import { useMemo } from 'react'
import Markdown from 'react-markdown'
import { VitalChart } from './VitalChart'
import { parseChartBlocks } from '../lib/chartParser'

interface MessageContentProps {
  content: string
  streaming: boolean
}

export function MessageContent({ content, streaming }: MessageContentProps) {
  if (streaming) {
    return (
      <div className="content">
        {content}
        <span className="streaming-cursor" />
      </div>
    )
  }

  const segments = useMemo(() => parseChartBlocks(content), [content])

  return (
    <div className="content markdown-body">
      {segments.map((segment, i) => {
        if (segment.type === 'chart') {
          return <VitalChart key={i} spec={segment.spec} />
        }
        return <Markdown key={i}>{segment.content}</Markdown>
      })}
    </div>
  )
}
