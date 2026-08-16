import { useRef, useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { Database, Download, HardDriveUpload, RefreshCw } from "lucide-react";
import { api } from "../lib/api";

const CHUNK_SIZE = 1024 * 1024; // 1MB

export default function Maintenance() {
  const { data: sizeData } = useQuery({ queryKey: ["db-size"], queryFn: api.dbSize });
  const fileRef = useRef<HTMLInputElement>(null);
  const [status, setStatus] = useState("");
  const [progress, setProgress] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);

  const size = sizeData ? sizeData.total : 0;

  const fmtSize = (n: number) =>
    n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(2)} GiB` : n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(2)} MiB` : `${(n / 1024).toFixed(1)} KiB`;

  const vacuum = useMutation({
    mutationFn: api.dbVacuum,
    onSuccess: () => setStatus("VACUUM 完成"),
    onError: (e) => setStatus("VACUUM 失败: " + (e as Error).message),
  });

  const download = async () => {
    setStatus("正在生成一致性快照…");
    try {
      const blob = await api.backupDownload();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `argus-backup-${new Date().toISOString().replace(/[:.]/g, "-")}.db`;
      a.click();
      URL.revokeObjectURL(url);
      setStatus("备份下载完成");
    } catch (e) {
      setStatus("备份失败: " + (e as Error).message);
    }
  };

  // 计算文件 SHA-256（用于恢复校验）
  const sha256File = async (file: File): Promise<string> => {
    const buf = await file.arrayBuffer();
    const digest = await crypto.subtle.digest("SHA-256", buf);
    return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
  };

  const restore = async (file: File) => {
    setBusy(true);
    setProgress(0);
    setStatus("正在校验并分片上传…");
    try {
      const totalHash = await sha256File(file);
      const uploadId = `r-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
      const chunks = Math.ceil(file.size / CHUNK_SIZE);
      let offset = 0;
      for (let i = 0; i < chunks; i++) {
        const slice = file.slice(offset, offset + CHUNK_SIZE);
        const final = i === chunks - 1;
        const r = await api.backupRestore(new File([slice], file.name), uploadId, offset, final, totalHash);
        offset = r.written;
        setProgress(Math.round((offset / file.size) * 100));
        if (r.final) {
          setStatus(r.note || "恢复完成");
          setBusy(false);
          return;
        }
      }
    } catch (e) {
      setStatus("恢复失败: " + (e as Error).message);
      setBusy(false);
    }
  };

  const onPick = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (f) restore(f);
  };

  return (
    <div>
      <h1 className="mb-1 flex items-center gap-2 text-xl font-semibold">
        <Database className="h-5 w-5 text-accent" /> 备份与维护
      </h1>
      <p className="mb-5 text-sm text-muted">一致性快照备份、安全分片恢复与数据库维护</p>

      <div className="mb-4 max-w-md rounded-xl border border-border bg-panel p-5">
        <div className="mb-3 flex items-center justify-between text-sm">
          <span className="text-muted">数据库体积</span>
          <span className="tabular font-medium">{fmtSize(size)}</span>
        </div>
        <div className="flex flex-wrap gap-2">
          <button onClick={download} disabled={busy} className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white disabled:opacity-40">
            <Download className="h-4 w-4" /> 下载备份
          </button>
          <button onClick={() => vacuum.mutate()} disabled={busy} className="flex items-center gap-2 rounded-lg border border-border px-4 py-2 text-sm disabled:opacity-40">
            <RefreshCw className="h-4 w-4" /> VACUUM
          </button>
          <button
            onClick={() => fileRef.current?.click()}
            disabled={busy}
            className="flex items-center gap-2 rounded-lg border border-err/40 px-4 py-2 text-sm text-err disabled:opacity-40"
          >
            <HardDriveUpload className="h-4 w-4" /> 恢复备份
          </button>
          <input ref={fileRef} type="file" accept=".db" className="hidden" onChange={onPick} />
        </div>
        {progress !== null && (
          <div className="mt-3">
            <div className="mb-1 text-xs text-muted">上传 {progress}%</div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
              <div className="h-full rounded-full bg-accent transition-all" style={{ width: `${progress}%` }} />
            </div>
          </div>
        )}
        {status && <p className="mt-3 break-all text-sm text-muted">{status}</p>}
        <p className="mt-3 text-xs text-muted">
          恢复采用分片校验 + SQLite 完整性检查 + 原子切换；恢复后需重启服务生效，原库会保留为
          .pre-restore-* 备份。
        </p>
      </div>
    </div>
  );
}
