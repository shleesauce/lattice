import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  /** Short label for what failed, shown in the fallback (e.g. "the workspace"). */
  scope?: string
}
interface State {
  error: Error | null
}

/**
 * ErrorBoundary catches render-time throws so one bad component can't white-screen
 * the whole fleet console — the worst failure for a control room you reach from a
 * phone mid-incident. Renders a token-styled fallback with a reload, and logs the
 * error (incl. component stack) to the console for diagnosis.
 *
 * Wrap the root App, and again (with scope) around the lazy Workspace chunk so a
 * Workspace crash leaves the fleet view usable.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Lattice: render error caught by ErrorBoundary', error, info.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    const what = this.props.scope ? `${this.props.scope} ran into an error` : 'Lattice ran into an error'
    return (
      <div
        role="alert"
        style={{
          minHeight: this.props.scope ? '40vh' : '100vh',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          background: 'var(--base)',
          color: 'var(--fg-1)',
          fontFamily: 'var(--font-ui)',
          padding: 24,
        }}
      >
        <div
          style={{
            maxWidth: 460,
            width: '100%',
            background: 'var(--raised)',
            border: '1px solid var(--border)',
            borderRadius: 12,
            padding: '22px 24px',
          }}
        >
          <div style={{ fontWeight: 600, fontSize: 15, marginBottom: 6 }}>{what}</div>
          <div style={{ color: 'var(--fg-3)', fontSize: 13, lineHeight: 1.5, marginBottom: 16 }}>
            The console hit an unexpected error and stopped rendering. Your fleet and sessions are
            unaffected — reload to recover.
          </div>
          {this.state.error.message && (
            <pre
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 11,
                color: 'var(--fg-3)',
                background: 'var(--base)',
                border: '1px solid var(--border)',
                borderRadius: 8,
                padding: '8px 10px',
                margin: '0 0 16px',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                maxHeight: 120,
                overflow: 'auto',
              }}
            >
              {this.state.error.message}
            </pre>
          )}
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              type="button"
              onClick={() => window.location.reload()}
              style={{
                background: 'var(--teal)',
                color: 'var(--base)',
                border: 'none',
                borderRadius: 8,
                padding: '8px 14px',
                fontWeight: 600,
                fontSize: 13,
                cursor: 'pointer',
              }}
            >
              Reload
            </button>
            {this.props.scope && (
              <button
                type="button"
                onClick={() => this.setState({ error: null })}
                style={{
                  background: 'transparent',
                  color: 'var(--fg-2)',
                  border: '1px solid var(--border)',
                  borderRadius: 8,
                  padding: '8px 14px',
                  fontSize: 13,
                  cursor: 'pointer',
                }}
              >
                Try again
              </button>
            )}
          </div>
        </div>
      </div>
    )
  }
}
