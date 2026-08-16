import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Download, File as FileIcon, Folder, RefreshCw, Trash2, Upload } from "lucide-react";
import { api, type FsEntry } from "../lib/api";
import { useServers } from "../context/servers";
import { fmtBytes } from "../lib/format";
import { useI18n } from "../lib/i18n";

export default function Files() {
  const { t, fmtDateTime } = useI18n();
  const { servers } = useServers();
  const qc = useQueryClient();
  const [serverId, setServerId] = useState<number>(servers[0]?.id ?? 0);
  const [path, setPath] = useState("/");
  const [selected, setSelected] = useState<FsEntry | null>(null);
  const [preview, setPreview] = useState<string>("");
  const [uploading, setUploading] = useState(false);

  const { data, refetch, isFetching } = useQuery({
    queryKey: ["files", serverId, path],
    queryFn: () => api.files(serverId, path),
    enabled: serverId > 0,
  });
  const entries = data?.entries ?? [];

  const read = useMutation({
    mutationFn: (e: FsEntry) => api.fileRead(serverId, e.path, 0, 65536),
    onSuccess: (r) => {
      try {
        setPreview(new TextDecoder().decode(Uint8Array.from(atob(r.data), (c) => c.charCodeAt(0))));
      } catch {
        setPreview(t("files.binaryFile", { size: fmtBytes(r.size) }));
      }
    },
  });

  const del = useMutation({
    mutationFn: (e: FsEntry) => api.fileDelete(serverId, e.path, e.is_dir),
    onSuccess: () => refetch(),
  });

  const upload = useMutation({
    mutationFn: async (file: File) => {
      const buf = await file.arrayBuffer();
      const b64 = btoa(String.fromCharCode(...new Uint8Array(buf)));
      await api.fileWrite(serverId, path === "/" ? "/" + file.name : path + "/" + file.name, b64);
    },
    onSuccess: () => refetch(),
    onSettled: () => setUploading(false),
  });

  const nav = (p: string) => setPath(p);
  const up = () => {
    const parent = path === "/" ? "/" : path.slice(0, path.lastIndexOf("/")) || "/";
    setPath(parent);
  };

  return (
    <div>
      <div className="mb-5 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{t("files.title")}</h1>
          <p className="text-sm text-muted">{t("files.subtitle")}</p>
        </div>
        <select
          value={serverId}
          onChange={(e) => {
            setServerId(Number(e.target.value));
            setPath("/");
          }}
          className="rounded-lg border border-border bg-panel px-3 py-2 text-sm outline-none"
        >
          {servers.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </div>

      {/* 路径导航 */}
      <div className="mb-3 flex items-center gap-2 rounded-xl border border-border bg-panel p-3">
        <button onClick={up} className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5" title={t("files.upDir")}>
          <ArrowLeft className="h-4 w-4" />
        </button>
        <span className="flex-1 truncate font-mono text-sm">{path}</span>
        <button onClick={() => refetch()} className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5" title={t("common.refresh")}>
          <RefreshCw className={`h-4 w-4 ${isFetching ? "animate-spin" : ""}`} />
        </button>
        <label className="flex cursor-pointer items-center gap-1 rounded-lg bg-accent px-3 py-1.5 text-sm text-white hover:opacity-90">
          <Upload className="h-3.5 w-3.5" />
          {t("files.upload")}
          <input
            type="file"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) {
                setUploading(true);
                upload.mutate(f);
              }
            }}
          />
        </label>
      </div>

      {/* 文件列表 */}
      <div className="overflow-hidden rounded-xl border border-border bg-panel">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2.5 font-normal">{t("common.name")}</th>
              <th className="px-4 py-2.5 font-normal">{t("files.size")}</th>
              <th className="px-4 py-2.5 font-normal">{t("files.permissions")}</th>
              <th className="px-4 py-2.5 font-normal">{t("files.modified")}</th>
              <th className="px-4 py-2.5 text-right font-normal">{t("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e) => (
              <tr
                key={e.path}
                className="cursor-pointer border-b border-border last:border-0 hover:bg-black/2 dark:hover:bg-white/2"
                onClick={() => (e.is_dir ? nav(e.path) : setSelected(e))}
              >
                <td className="px-4 py-2.5">
                  <span className="flex items-center gap-2">
                    {e.is_dir ? <Folder className="h-4 w-4 text-warn" /> : <FileIcon className="h-4 w-4 text-muted" />}
                    {e.name}
                  </span>
                </td>
                <td className="px-4 py-2.5 tabular text-muted">{e.is_dir ? "—" : fmtBytes(e.size)}</td>
                <td className="px-4 py-2.5 font-mono text-xs text-muted">{e.mode}</td>
                <td className="px-4 py-2.5 tabular text-muted">{fmtDateTime(e.modified)}</td>
                <td className="px-4 py-2.5">
                  <div className="flex justify-end gap-1">
                    {!e.is_dir && (
                      <button
                        onClick={(ev) => {
                          ev.stopPropagation();
                          read.mutate(e);
                        }}
                        title={t("files.preview")}
                        className="rounded p-1.5 hover:bg-black/5 dark:hover:bg-white/5"
                      >
                        <Download className="h-4 w-4" />
                      </button>
                    )}
                    <button
                      onClick={(ev) => {
                        ev.stopPropagation();
                        if (confirm(t(e.is_dir ? "files.confirmDeleteDir" : "files.confirmDeleteFile", { name: e.name }))) del.mutate(e);
                      }}
                      title={t("common.delete")}
                      className="rounded p-1.5 text-err hover:bg-err/10"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {entries.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-10 text-center text-muted">
                  {t("files.emptyDir")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 文件预览 */}
      {selected && (
        <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40" onClick={() => setSelected(null)}>
          <div className="w-full max-w-2xl rounded-xl border border-border bg-panel p-5" onClick={(e) => e.stopPropagation()}>
            <h2 className="mb-3 flex items-center justify-between text-sm font-medium">
              {selected.name}
              <button
                onClick={() => {
                  read.mutate(selected);
                }}
                className="rounded-lg border border-border px-3 py-1 text-xs"
              >
                {t("files.loadContent")}
              </button>
            </h2>
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded-lg bg-black/5 p-3 text-xs dark:bg-white/5">
              {preview || t("files.hintLoad")}
            </pre>
            <div className="mt-3 flex justify-end">
              <button onClick={() => setSelected(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
                {t("common.close")}
              </button>
            </div>
          </div>
        </div>
      )}
      {uploading && (
        <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40">
          <div className="rounded-xl border border-border bg-panel p-5 text-sm">{t("files.uploading")}</div>
        </div>
      )}
    </div>
  );
}
