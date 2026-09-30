import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Table, TableActionCell, TableActionHead, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { createBuildOAuthProxy, deleteBuildOAuthProxy, getBuildOAuthProxyURL, listBuildOAuthProxies, updateBuildOAuthProxy, type BuildOAuthProxyDTO } from "@/features/settings/settings-api";
import { ErrorState, TableLoadingRow } from "@/shared/components/data-state";

type ProxyForm = { name: string; proxyURL: string; enabled: boolean };
const emptyForm: ProxyForm = { name: "", proxyURL: "", enabled: true };

export function BuildOAuthProxies() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<BuildOAuthProxyDTO | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<BuildOAuthProxyDTO | undefined>();
  const [form, setForm] = useState<ProxyForm>(emptyForm);
  const [proxyVisible, setProxyVisible] = useState(false);
  const [revealedProxyURL, setRevealedProxyURL] = useState("");
  const query = useQuery({
    queryKey: ["build-oauth-proxies"],
    queryFn: listBuildOAuthProxies,
  });

  const save = useMutation({
    mutationFn: () => {
      const proxyURL = form.proxyURL.trim();
      const input = { name: form.name.trim(), enabled: form.enabled, proxyURL: proxyURL && proxyURL !== revealedProxyURL ? proxyURL : undefined };
      return editing ? updateBuildOAuthProxy(editing.id, input) : createBuildOAuthProxy({ ...input, proxyURL });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
      setEditing(undefined);
      toast.success(t("buildOAuthProxies.saved"));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const reveal = useMutation({
    mutationFn: () => {
      if (!editing) throw new Error(t("buildOAuthProxies.revealUnavailable"));
      return getBuildOAuthProxyURL(editing.id);
    },
    onSuccess: ({ proxyURL }) => {
      setRevealedProxyURL(proxyURL);
      setProxyVisible(true);
      setForm((current) => ({ ...current, proxyURL }));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const toggle = useMutation({
    mutationFn: (value: BuildOAuthProxyDTO) => updateBuildOAuthProxy(value.id, { name: value.name, enabled: !value.enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteBuildOAuthProxy(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
      setDeleting(undefined);
      toast.success(t("buildOAuthProxies.deleted"));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });

  function openCreate() {
    setForm(emptyForm);
    setProxyVisible(false);
    setRevealedProxyURL("");
    setEditing(null);
  }

  function openEdit(value: BuildOAuthProxyDTO) {
    setForm({ name: value.name, proxyURL: "", enabled: value.enabled });
    setProxyVisible(false);
    setRevealedProxyURL("");
    setEditing(value);
  }

  return (
    <section className="space-y-3">
      <div className="flex min-h-8 items-center justify-between gap-3 px-1">
        <div className="min-w-0">
          <h2 className="text-sm font-medium tracking-tight">{t("buildOAuthProxies.title")}</h2>
          <p className="mt-1 max-w-xl text-xs leading-5 text-muted-foreground">{t("buildOAuthProxies.description")}</p>
        </div>
        <Button type="button" size="sm" variant="secondary" onClick={openCreate}><Plus />{t("buildOAuthProxies.add")}</Button>
      </div>
      <div className="overflow-x-auto rounded-md border">
        {query.isError ? <ErrorState message={query.error.message} onRetry={() => void query.refetch()} /> : null}
        {!query.isError ? (
          <Table className="table-fixed">
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[28%]">{t("buildOAuthProxies.name")}</TableHead>
                <TableHead>{t("buildOAuthProxies.endpoint")}</TableHead>
                <TableHead className="w-24">{t("buildOAuthProxies.enabled")}</TableHead>
                <TableActionHead />
              </TableRow>
            </TableHeader>
            {query.isPending ? <TableBody><TableLoadingRow colSpan={4} /></TableBody> : null}
            {!query.isPending && (query.data?.items.length ?? 0) === 0 ? (
              <TableBody>
                <TableRow>
                  <TableCell colSpan={4} className="h-24 text-center text-xs text-muted-foreground">{t("buildOAuthProxies.empty")}</TableCell>
                </TableRow>
              </TableBody>
            ) : null}
            {!query.isPending ? (
              <TableBody>
                {query.data?.items.map((value) => (
                  <TableRow key={value.id}>
                    <TableCell>
                      <span className="block truncate text-xs font-medium" title={value.name}>{value.name}</span>
                      {value.accountBoundProxy ? <p className="text-[10px] text-muted-foreground">{t("buildOAuthProxies.accountBound")}</p> : null}
                    </TableCell>
                    <TableCell>
                      <div className="min-w-0" title={`${value.proxyDisplay || ""} · ${value.proxyFingerprint || ""}`}>
                        <p className="truncate text-xs font-medium">{value.proxyDisplay}</p>
                        {value.proxyFingerprint ? <p className="font-mono text-[10px] text-muted-foreground">#{value.proxyFingerprint}</p> : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Switch checked={value.enabled} disabled={toggle.isPending} onCheckedChange={() => toggle.mutate(value)} aria-label={t("buildOAuthProxies.enabled")} />
                    </TableCell>
                    <TableActionCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button type="button" variant="ghost" size="icon" className="size-8" aria-label={t("common.actions")}><MoreHorizontal /></Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => openEdit(value)}><Pencil />{t("common.edit")}</DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={() => setDeleting(value)}><Trash2 />{t("common.delete")}</DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableActionCell>
                  </TableRow>
                ))}
              </TableBody>
            ) : null}
          </Table>
        ) : null}
      </div>

      <Dialog open={editing !== undefined} onOpenChange={(open) => { if (!open) setEditing(undefined); }}>
        <DialogContent className="overflow-y-auto sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{editing ? t("buildOAuthProxies.editTitle") : t("buildOAuthProxies.addTitle")}</DialogTitle>
            <DialogDescription>{t("buildOAuthProxies.dialogDescription")}</DialogDescription>
          </DialogHeader>
          <form className="space-y-3.5" onSubmit={(event) => { event.preventDefault(); event.stopPropagation(); save.mutate(); }}>
            <div className="space-y-2">
              <Label htmlFor="build-oauth-proxy-name">{t("buildOAuthProxies.name")}</Label>
              <Input id="build-oauth-proxy-name" autoComplete="off" data-1p-ignore data-lpignore="true" data-form-type="other" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="build-oauth-proxy-url">{t("settings.egress.proxyURL")}</Label>
              <div className="flex gap-2">
                <Input id="build-oauth-proxy-url" type={proxyVisible ? "text" : "password"} autoComplete="off" data-1p-ignore data-lpignore="true" data-form-type="other" placeholder={editing ? t("settings.egress.keepConfigured") : "socks5h://US.{account}:token@host:2260"} value={form.proxyURL} onChange={(event) => setForm({ ...form, proxyURL: event.target.value })} />
                {editing ? (
                  <Button type="button" variant="outline" size="icon" className="shrink-0" disabled={reveal.isPending} aria-label={t(proxyVisible ? "buildOAuthProxies.hide" : "buildOAuthProxies.reveal")} onClick={() => { if (revealedProxyURL) setProxyVisible((visible) => !visible); else reveal.mutate(); }}>
                    {reveal.isPending ? <Spinner /> : proxyVisible ? <EyeOff /> : <Eye />}
                  </Button>
                ) : null}
              </div>
              <p className="text-xs leading-5 text-muted-foreground">{t("buildOAuthProxies.proxyHelp")}</p>
            </div>
            <div className="flex min-h-10 items-center justify-between gap-4 rounded-md bg-muted/45 px-3">
              <Label htmlFor="build-oauth-proxy-enabled" className="text-xs font-medium">{t("buildOAuthProxies.enabled")}</Label>
              <Switch id="build-oauth-proxy-enabled" checked={form.enabled} onCheckedChange={(enabled) => setForm({ ...form, enabled })} />
            </div>
            <DialogFooter>
              <Button type="button" variant="secondary" size="sm" onClick={() => setEditing(undefined)}>{t("common.cancel")}</Button>
              <Button type="submit" size="sm" disabled={save.isPending || !form.name.trim() || (!editing && !form.proxyURL.trim())}>{save.isPending ? <Spinner /> : null}{t("common.save")}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleting)} onOpenChange={(next) => { if (!next) setDeleting(undefined); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("buildOAuthProxies.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("buildOAuthProxies.deleteDescription", { name: deleting?.name })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction className="bg-destructive text-white hover:bg-destructive/90" disabled={remove.isPending} onClick={(event) => { event.preventDefault(); if (deleting) remove.mutate(deleting.id); }}>
              {remove.isPending ? <Spinner /> : null}{t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
