import { Component, type ReactNode } from 'react'

export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() {
    if (this.state.failed) return <main className="fatal" role="alert"><h1>This view could not load.</h1><p>Reload the console to try again.</p><button onClick={() => window.location.reload()}>Reload console</button></main>
    return this.props.children
  }
}
