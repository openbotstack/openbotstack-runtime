import { createContext, useContext, useState, useEffect, type ReactNode } from 'react'

interface ExecutionViewerContextType {
  executionId: string | null
  openViewer: (executionId: string) => void
  closeViewer: () => void
}

const ExecutionViewerContext = createContext<ExecutionViewerContextType>({
  executionId: null,
  openViewer: () => {},
  closeViewer: () => {},
})

export function ExecutionViewerProvider({ children }: { children: ReactNode }) {
  const [executionId, setExecutionId] = useState<string | null>(null)

  useEffect(() => {
    if (import.meta.env.DEV) {
      (window as any).__obsOpenViewer = setExecutionId
      return () => { delete (window as any).__obsOpenViewer }
    }
  }, [])

  return (
    <ExecutionViewerContext.Provider
      value={{
        executionId,
        openViewer: setExecutionId,
        closeViewer: () => setExecutionId(null),
      }}
    >
      {children}
    </ExecutionViewerContext.Provider>
  )
}

export function useExecutionViewer() {
  return useContext(ExecutionViewerContext)
}
