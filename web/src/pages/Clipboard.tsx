import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ClipboardList, Copy, Pencil, Plus, Trash2 } from "lucide-react";
import { api, type ClipboardItem } from "../lib/api";
import { useI18n } from "../lib/i18n";

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted">{label}</span>
      {children}
    </label>
  );
}

const inputCls = "rounded-lg border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent";

interface FormState {
  id?: number;
  title: string;
  content: string;
}

const emptyForm: FormState = { title: "", content: "" };

export default function Clipboard() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["clipboard"], queryFn: api.clipboard });
  const items = data?.items ?? [];
  const [form, setForm] = useState<FormState | null>(null);
  const [error, setError] = useState("");
  const [copiedId, setCopiedId] = useState<number | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["clipboard"] });

  const save = useMutation({
    mutationFn: async (f: FormState) => {
      if (f.id) return api.updateClipboard(f.id, { title: f.title || undefined, content: f.content });
      return api.createClipboard({ title: f.title || undefined, content: f.content });
    },
    onSuccess: () => {
      setForm(null);
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });

  const remove = useMutation({
    mutationFn: api.deleteClipboard,
    onSuccess: () => {
      setError("");
      invalidate();
    },
    onError: (e) => setError((e as Error).message),
  });

  const copy = (item: ClipboardItem) => {
    navigator.clipboard.writeText(item.content).then(
      () => {
        setCopiedId(item.id);
        setTimeout(() => setCopiedId(null), 1500);
      },
      () => setError(t("clipboard.copyFailed")),
    );
  };

  return (
    <div>
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{t("clipboard.title")}</h1>
          <p className="text-sm text-muted">{t("clipboard.subtitle")}</p>
        </div>
        <button
          onClick={() => setForm(emptyForm)}
          className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm text-white"
        >
          <Plus className="h-4 w-4" /> {t("clipboard.newItem")}
        </button>
      </div>

      {error && <p className="mb-3 text-sm text-err">{error}</p>}

      {form && (
        <div className="mb-5 rounded-xl border border-border bg-panel p-4">
          <h2 className="mb-3 text-sm font-medium">{form.id ? t("clipboard.editItem") : t("clipboard.newItem")}</h2>
          <div className="grid grid-cols-1 gap-3">
            <Field label={t("clipboard.titleLabel")}>
              <input
                value={form.title}
                onChange={(e) => setForm({ ...form, title: e.target.value })}
                placeholder={t("clipboard.titlePlaceholder")}
                className={inputCls}
              />
            </Field>
            <Field label={t("clipboard.content")}>
              <textarea
                value={form.content}
                onChange={(e) => setForm({ ...form, content: e.target.value })}
                placeholder={t("clipboard.contentPlaceholder")}
                rows={6}
                className={`${inputCls} resize-y`}
              />
            </Field>
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => save.mutate(form)}
              disabled={!form.content.trim()}
              className="rounded-lg bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-40"
            >
              {t("common.save")}
            </button>
            <button onClick={() => setForm(null)} className="rounded-lg border border-border px-4 py-1.5 text-sm text-muted">
              {t("common.cancel")}
            </button>
          </div>
        </div>
      )}

      <div className="space-y-3">
        {items.map((item) => (
          <div key={item.id} className="rounded-xl border border-border bg-panel p-4">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <ClipboardList className="h-4 w-4 text-muted" />
                <span className="text-sm font-medium">{item.title || t("clipboard.untitled")}</span>
              </div>
              <div className="flex items-center gap-1">
                <button
                  onClick={() => copy(item)}
                  title={t("clipboard.copy")}
                  className="flex items-center gap-1 rounded-lg px-2 py-1 text-sm text-accent hover:bg-accent/10"
                >
                  {copiedId === item.id ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  {copiedId === item.id ? t("clipboard.copied") : t("clipboard.copy")}
                </button>
                <button
                  onClick={() => setForm({ id: item.id, title: item.title, content: item.content })}
                  title={t("common.edit")}
                  className="rounded-lg p-1.5 text-muted hover:bg-black/5 dark:hover:bg-white/5"
                >
                  <Pencil className="h-4 w-4" />
                </button>
                <button
                  onClick={() => confirm(t("clipboard.confirmDelete")) && remove.mutate(item.id)}
                  title={t("common.delete")}
                  className="rounded-lg p-1.5 text-err hover:bg-err/10"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
            <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-bg p-3 text-xs">
              {item.content}
            </pre>
          </div>
        ))}
        {items.length === 0 && (
          <div className="rounded-xl border border-border bg-panel py-12 text-center text-sm text-muted">
            {t("clipboard.empty")}
          </div>
        )}
      </div>
    </div>
  );
}
