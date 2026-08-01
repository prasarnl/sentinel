import { Component, type ErrorInfo, type ReactNode } from "react";
import { AlertTriangle } from "lucide-react";

interface Props {
  children: ReactNode;
  /** Names the area that failed, so the message says what is broken. */
  label?: string;
}

interface State {
  error: Error | null;
}

/** Contains a render error to one part of the page.
 *
 * React unmounts the entire tree when a render throws, so before this a single
 * bad field turned the whole dashboard blank with nothing on screen to explain
 * it — the failure looked like the app dying rather than one panel breaking.
 * A monitoring tool in particular should degrade to "this panel is broken"
 * rather than to a white page, since the rest of the data is still good. */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Kept in the console so the stack is recoverable after the fact; the
    // UI deliberately shows only the message.
    console.error("render error", this.props.label ?? "", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <div className="rounded-lg border border-[var(--border)] bg-[var(--surface-1)] p-4">
        <div className="flex gap-3">
          <AlertTriangle size={16} className="mt-0.5 shrink-0 text-[var(--series-6)]" />
          <div className="min-w-0">
            <p className="text-sm font-medium">
              {this.props.label ? `${this.props.label} failed to render` : "Something went wrong"}
            </p>
            <p className="mt-1 text-sm text-[var(--text-secondary)]">
              The rest of the page is unaffected. Details are in the browser console.
            </p>
            <p className="mt-2 break-words font-mono text-xs text-[var(--text-muted)]">
              {this.state.error.message}
            </p>
          </div>
        </div>
      </div>
    );
  }
}
