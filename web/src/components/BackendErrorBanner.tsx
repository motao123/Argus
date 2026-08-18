// 后端不可达提示（借鉴 dash-v2 BackendErrorState）：请求网络层失败时
// 显示顶部横幅，后端恢复后自动隐藏。
import { useEffect, useState } from "react";
import { WifiOff } from "lucide-react";
import { useI18n } from "../lib/i18n";

export default function BackendErrorBanner() {
  const { t } = useI18n();
  const [down, setDown] = useState(false);

  useEffect(() => {
    const onDown = () => setDown(true);
    const onUp = () => setDown(false);
    window.addEventListener("argus:backend-unreachable", onDown);
    window.addEventListener("argus:backend-reachable", onUp);
    return () => {
      window.removeEventListener("argus:backend-unreachable", onDown);
      window.removeEventListener("argus:backend-reachable", onUp);
    };
  }, []);

  if (!down) return null;
  return (
    <div className="fixed inset-x-0 top-0 z-[60] flex items-center justify-center gap-2 bg-err/90 px-4 py-2 text-sm text-white shadow-lg">
      <WifiOff className="h-4 w-4" />
      <span>{t("common.backendUnreachable")}</span>
    </div>
  );
}
