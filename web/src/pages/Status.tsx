import { Link } from "react-router-dom";

export function NotFound() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-3xl font-bold text-muted">404</h1>
      <p className="text-sm text-muted">页面不存在</p>
      <Link to="/" className="rounded-lg bg-accent px-4 py-2 text-sm text-white">返回首页</Link>
    </div>
  );
}

export function Forbidden() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-3xl font-bold text-muted">403</h1>
      <p className="text-sm text-muted">没有权限访问该页面</p>
      <Link to="/admin/overview" className="rounded-lg bg-accent px-4 py-2 text-sm text-white">返回后台</Link>
    </div>
  );
}
