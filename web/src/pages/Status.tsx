import { Link } from "react-router-dom";
import { useI18n } from "../lib/i18n";

export function NotFound() {
  const { t } = useI18n();
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-3xl font-bold text-muted">404</h1>
      <p className="text-sm text-muted">{t("status.notFound")}</p>
      <Link to="/" className="rounded-lg bg-accent px-4 py-2 text-sm text-white">{t("status.backHome")}</Link>
    </div>
  );
}

export function Forbidden() {
  const { t } = useI18n();
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-3xl font-bold text-muted">403</h1>
      <p className="text-sm text-muted">{t("status.forbidden")}</p>
      <Link to="/admin/overview" className="rounded-lg bg-accent px-4 py-2 text-sm text-white">{t("status.backAdmin")}</Link>
    </div>
  );
}
