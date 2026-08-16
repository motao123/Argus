import { Component, type ReactNode } from "react";
import { I18nContext, type I18nContextValue } from "../lib/i18n";

interface Props {
  children: ReactNode;
}
interface State {
  error: Error | null;
}

// 全局错误边界：渲染错误时不白屏，提供重试。
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static contextType = I18nContext;
  declare context: I18nContextValue;

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  render() {
    const t = this.context?.t ?? ((key: string) => key);
    if (this.state.error) {
      return (
        <div className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
          <h1 className="text-xl font-semibold">{t("status.pageError")}</h1>
          <p className="max-w-md break-all text-sm text-muted">{this.state.error.message}</p>
          <button
            onClick={() => this.setState({ error: null })}
            className="rounded-lg bg-accent px-4 py-2 text-sm text-white"
          >
            {t("common.retry")}
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
