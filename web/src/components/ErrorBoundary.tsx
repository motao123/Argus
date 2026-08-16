import { Component, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}
interface State {
  error: Error | null;
}

// 全局错误边界：渲染错误时不白屏，提供重试。
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  render() {
    if (this.state.error) {
      return (
        <div className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
          <h1 className="text-xl font-semibold">页面出错</h1>
          <p className="max-w-md break-all text-sm text-muted">{this.state.error.message}</p>
          <button
            onClick={() => this.setState({ error: null })}
            className="rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            重试
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
