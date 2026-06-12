import { useEffect, useMemo, useRef, useState } from 'react'
import Markdown from 'react-markdown'
import { VitalChart } from './VitalChart'
import { parseChartBlocks } from '../lib/chartParser'

interface MessageContentProps {
  content: string
  streaming: boolean
}

// Streaming markdown rendering with throttling.
//
// react-markdown re-parses its full input on every prop change. During token
// streaming the parent appends a token per SSE event, which would re-parse the
// whole message O(n) on every token — O(n²) over a long generation. To keep
// streaming smooth we buffer the latest content in a ref and only commit it to
// a displayed state on an animation frame (rAF), coalescing many token updates
// into one render per frame (~60fps). Once streaming ends we do a final
// synchronous commit so the complete text is shown immediately.
//
// References: Vercel AI SDK "MemoizedMarkdown" pattern; Chrome for Developers
// "Best practices to render streamed LLM responses".
export function MessageContent({ content, streaming }: MessageContentProps) {
  const [displayed, setDisplayed] = useState(content)
  const latest = useRef(content)
  const rafId = useRef<number | null>(null)

  useEffect(() => {
    latest.current = content

    if (!streaming) {
      // Final render: flush immediately and cancel any pending frame.
      if (rafId.current !== null) {
        cancelAnimationFrame(rafId.current)
        rafId.current = null
      }
      setDisplayed(content)
      return
    }

    // Coalesce token bursts into one render per animation frame.
    if (rafId.current !== null) return
    rafId.current = requestAnimationFrame(() => {
      rafId.current = null
      setDisplayed(latest.current)
    })
  }, [content, streaming])

  useEffect(() => () => {
    if (rafId.current !== null) cancelAnimationFrame(rafId.current)
  }, [])

  const segments = useMemo(() => parseChartBlocks(displayed), [displayed])

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
