import { Component, type ReactNode } from 'react'

// ErrorBoundary prevents a runtime error in one widget from blanking the whole
// dashboard. It shows a small inline error so the rest of the grid keeps going.
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="h-full flex items-center justify-center p-3">
          <div className="text-xs text-(--danger) text-center">
            <div className="mb-1 font-semibold">Widget error</div>
            <div className="text-(--text-faint)">{this.state.error.message}</div>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
