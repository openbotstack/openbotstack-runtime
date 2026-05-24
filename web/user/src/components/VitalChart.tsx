import {
  LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, Legend, ResponsiveContainer,
} from 'recharts'
import type { ChartSpec } from '../lib/chartParser'

interface VitalChartProps {
  spec: ChartSpec
}

export function VitalChart({ spec }: VitalChartProps) {
  if (!spec.data?.length || !spec.series?.length) {
    return <pre className="chart-error">Invalid chart data</pre>
  }

  return (
    <div className="chart-container">
      <div className="chart-title">{spec.title}</div>
      <ResponsiveContainer width="100%" height={280}>
        <LineChart data={spec.data} margin={{ top: 5, right: 20, bottom: 5, left: 10 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
          <XAxis dataKey={spec.xAxis.dataKey} stroke="#94a3b8" fontSize={12} />
          <YAxis stroke="#94a3b8" fontSize={12} />
          <Tooltip
            contentStyle={{ background: '#1e293b', border: '1px solid #334155', borderRadius: 6, color: '#e2e8f0' }}
          />
          <Legend wrapperStyle={{ color: '#94a3b8', fontSize: 12 }} />
          {spec.series.map((s) => (
            <Line
              key={s.dataKey}
              type="monotone"
              dataKey={s.dataKey}
              name={s.name}
              stroke={s.color}
              strokeWidth={2}
              dot={{ r: 3 }}
              activeDot={{ r: 5 }}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
